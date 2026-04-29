// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"log/slog"

	"github.com/cockroachdb/errors"
	"github.com/google/cel-go/cel"
	"github.com/labstack/echo/v4"

	"github.com/confighub/sdk/core/configkit"
	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/third_party/gaby"
	"github.com/confighub/sdk/core/workerapi"
)

// ToolchainProvider combines the ConfigConverter and ResourceProvider interfaces.
// All built-in resource providers (k8skit, cubkit, etc.) implement both.
type ToolchainProvider interface {
	configkit.ConfigConverter
	yamlkit.ResourceProvider
	GetToolchainType() workerapi.ToolchainType
}

// FunctionRegistry defines the interface for registering functions.
// This allows decoupling internal packages from the concrete FunctionHandler implementation.
type FunctionRegistry interface {
	RegisterFunction(functionName string, registration *FunctionRegistration) error
	GetHandlerImplementation(functionName string) FunctionImplementation
	GetPathRegistry() api.AttributeNameToResourceTypeToPathToVisitorInfoType
	GetAttributes() []api.AttributeName
	GetConverter() configkit.ConfigConverter
	GetResourceProvider() yamlkit.ResourceProvider
}

type FunctionHandler struct {
	functionMap      map[string]*FunctionRegistration
	converter        configkit.ConfigConverter
	resourceProvider yamlkit.ResourceProvider
}

// Ensure FunctionHandler implements FunctionRegistry
var _ FunctionRegistry = (*FunctionHandler)(nil)

// NewFunctionHandler creates a new FunctionHandler with the given provider.
// The provider supplies the converter and resource provider, and the
// compute-mutations function is automatically registered.
func NewFunctionHandler(provider ToolchainProvider) *FunctionHandler {
	fh := &FunctionHandler{}
	fh.functionMap = make(map[string]*FunctionRegistration)
	fh.converter = provider
	fh.resourceProvider = provider
	fh.registerComputeMutations()
	return fh
}

func (fh *FunctionHandler) GetHandlerImplementation(functionName string) FunctionImplementation {
	registration, ok := fh.functionMap[functionName]
	if !ok {
		return nil
	}
	return registration.Function
}

func (fh *FunctionHandler) GetConverter() configkit.ConfigConverter {
	return fh.converter
}

func (fh *FunctionHandler) GetResourceProvider() yamlkit.ResourceProvider {
	return fh.resourceProvider
}

func (fh *FunctionHandler) Invoke(c echo.Context) error {
	var functionInvocation api.FunctionInvocationRequest
	err := c.Bind(&functionInvocation)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest,
			errors.Wrap(err, "bad function invocation request"))
	}

	resp, err := fh.InvokeCore(c.Request().Context(), &functionInvocation)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest,
			errors.Wrap(err, "functions couldn't execute on provided data"))
	}

	return c.JSON(http.StatusOK, resp) //nolint:wrapcheck // basic return
}

func (fh *FunctionHandler) InvokeCore(ctx context.Context, functionInvocation *api.FunctionInvocationRequest) (*api.FunctionInvocationResponse, error) {
	// TODO: Find a better place to initialize this.
	computeMutations, computeMutationsExists := fh.functionMap["compute-mutations"]
	if !computeMutationsExists {
		return nil, errors.New("the compute-mutations function does not exist")
	}

	functionContext := functionInvocation.FunctionContext

	// If functionContext.PreviousDataHash is set, it may not match the current ConfigData because
	// the data could have been modified by prior function executions, so don't overwrite it.
	// When operating on LiveState the PreviousDataHash is expected to be unset.
	if !functionContext.IsLiveState && functionContext.PreviousDataHash == "" {
		functionContext.PreviousDataHash = api.HashConfigDataSHA256(functionInvocation.ConfigData)
	}

	// Convert to YAML
	yamlData, err := fh.GetConverter().NativeToYAML(functionInvocation.ConfigData)
	if err != nil {
		return nil, err
	}
	serializedData := yamlData

	// Parse and validate WhereResource filter if provided
	functionOptions := api.NewFunctionOptions()
	functionOptions.WhereResourceExpressions, err = api.ParseAndValidateWhereResource(functionInvocation.WhereResource)
	if err != nil {
		return nil, err
	}

	// Pre-scan: verify functions exist and check if any need OtherData
	needsOtherData := false
	for _, invocation := range functionInvocation.FunctionInvocations {
		f, existed := fh.functionMap[invocation.FunctionName]
		if existed && len(f.OtherDataExpected) > 0 {
			needsOtherData = true
			break
		}
	}

	// Parse OtherData once before the loop if any function needs it
	var parsedOtherData map[api.OtherDataSource]gaby.Container
	if needsOtherData && len(functionInvocation.OtherData) > 0 {
		parsedOtherData = make(map[api.OtherDataSource]gaby.Container, len(functionInvocation.OtherData))
		for source, data := range functionInvocation.OtherData {
			otherYAML, err := fh.GetConverter().NativeToYAML(data)
			if err != nil {
				return nil, errors.Wrapf(err, "failed to convert OtherData[%s] to YAML", source)
			}
			parsed, err := gaby.ParseAll(otherYAML)
			if err != nil {
				return nil, errors.Wrapf(err, "failed to parse OtherData[%s]", source)
			}
			parsedOtherData[source] = parsed
		}
	}

	// Errors below are not wrapped here. They need to be wrapped at origin, if necessary.
	// The reason is so that we can return detailed error messages.
	success := true
	numFilters := functionInvocation.NumFilters
	messages := []string{}
	hasMutations := false
	mutations := []api.ResourceMutation{}
	mutators := []int{}
	outputs := make(map[api.OutputType]any)
	for functionIndex, invocation := range functionInvocation.FunctionInvocations {
		f, existed := fh.functionMap[invocation.FunctionName]
		if !existed {
			invocationInfo := fmt.Sprintf("invoke %s: function does not exist", invocation.FunctionName)
			slog.Info(invocationInfo)
			messages = append(messages, invocationInfo)
			success = false
			if functionInvocation.StopOnError {
				break
			}
			continue
		}

		isFilter := f.Validating && numFilters > 0
		if isFilter {
			numFilters--
		}

		invocationInfo := fmt.Sprintf("invoke %s", invocation.FunctionName)
		if slog.Default().Enabled(context.Background(), slog.LevelDebug) {
			// TODO: Decide whether sensitive Arguments should be supported
			for _, arg := range invocation.Arguments {
				// Note: Values can be quite large, such as for compute-mutations and patch-mutations
				valueStr := fmt.Sprintf(" %v", arg.Value)
				const maxValueLogLength = 1024
				if len(valueStr) > maxValueLogLength {
					// Truncate. This will break parsing of JSON or YAML, but it seems better than just a digest or length.
					valueStr = valueStr[:maxValueLogLength] + "..."
				}
				invocationInfo += valueStr
			}
		}

		arguments, validationErr := ValidateAndBuildArguments(fh.GetResourceProvider(), &functionInvocation.FunctionContext, &invocation, &f.FunctionSignature)
		if validationErr != nil {
			invocationInfo += ": " + validationErr.Error()
			slog.Info(invocationInfo)
			messages = append(messages, validationErr.Error())
			success = false
			if functionInvocation.StopOnError {
				break
			} else {
				continue
			}
		}

		// TODO: We shouldn't need to re-parse if the data hasn't changed.

		var newParsedData gaby.Container
		var functionOutput any
		var err error
		newParsedData, err = gaby.ParseAll(serializedData)
		if err != nil {
			return nil, errors.Wrap(err, "configuration data parsing error")
		}
		newParsedData, functionOutput, err = f.Function(FunctionImplementationArguments{
			FunctionContext: &functionContext,
			Options:         functionOptions,
			ParsedData:      newParsedData,
			ParsedOtherData: parsedOtherData,
			Arguments:       arguments,
		})
		if err == nil && isFilter {
			validationResult, ok := functionOutput.(api.ValidationResult)
			if !ok {
				err = errors.New("validating functions must return type ValidationResult")
			} else if !validationResult.Passed {
				invocationInfo += ": " + "did not pass"
				slog.Info(invocationInfo)
				break
			}
		}
		if err != nil {
			invocationInfo += ": " + err.Error()
			slog.Info(invocationInfo)
			messages = append(messages, err.Error())
			success = false
			if functionInvocation.StopOnError {
				break
			}
		} else {
			invocationInfo += ": succeeded"
			slog.Info(invocationInfo)
		}

		if f.Mutating {
			newSerializedDataString := newParsedData.String()
			newSerializedData := []byte(newSerializedDataString)
			if !bytes.Equal(serializedData, newSerializedData) {
				computeMutationArguments := []api.FunctionArgument{
					{Value: string(serializedData)},
					{Value: functionIndex},
					{Value: true}, // already converted to YAML
				}
				_, newMutationsOutput, err := computeMutations.Function(FunctionImplementationArguments{
					FunctionContext: &functionContext,
					Options:         nil, // mutations should see all resources
					ParsedData:      newParsedData,
					Arguments:       computeMutationArguments,
				})
				if err != nil {
					// TODO: It would be helpful to return the current configuration.
					return nil, errors.Wrap(err, "unable to compute mutations")
				}
				newMutations, ok := newMutationsOutput.(api.ResourceMutationList)
				if !ok {
					// log.Errorf("newMutations: %v", newMutationsOutput)
					return nil, errors.New("compute mutations returned invalid output")
				}
				// log.Debugf("%v", newMutations)
				var hasNewMutations bool
				mutations, hasNewMutations = yamlkit.AddMutations(mutations, newMutations)
				hasMutations = hasMutations || hasNewMutations
				mutators = append(mutators, functionIndex)
				serializedData = newSerializedData
			}
		}

		if functionOutput != nil && f.OutputInfo != nil {
			outputs, messages = api.CombineOutputs(
				f.FunctionName,
				functionContext.InstanceString(),
				f.OutputInfo.OutputType,
				outputs,
				functionOutput,
				functionIndex,
				messages,
			)
		}
	}

	var resp api.FunctionInvocationResponse
	resp.OrganizationID = functionInvocation.FunctionContext.OrganizationID
	resp.SpaceID = functionInvocation.FunctionContext.SpaceID
	resp.SpaceSlug = functionInvocation.FunctionContext.SpaceSlug
	resp.UnitID = functionInvocation.FunctionContext.UnitID
	resp.UnitSlug = functionInvocation.FunctionContext.UnitSlug
	resp.RevisionID = functionInvocation.FunctionContext.RevisionID

	// Convert from YAML back to the original format
	nativeData, err := fh.GetConverter().YAMLToNative(serializedData)
	// TODO: Handle this better
	if err != nil {
		return nil, err
	}
	resp.ConfigData = nativeData

	// Encode all outputs
	encodedOutputs := make(map[api.OutputType][]byte)
	for outputType, output := range outputs {
		if output != nil {
			encodedOutput, err := json.Marshal(output)
			if err != nil {
				messages = append(messages, fmt.Sprintf("failed to encode output for type %s: %v", outputType, err))
			} else {
				encodedOutputs[outputType] = encodedOutput
			}
		}
	}
	resp.Outputs = encodedOutputs
	resp.Success = success
	resp.HasNewMutations = hasMutations
	resp.Mutations = mutations
	resp.Mutators = mutators
	resp.ErrorMessages = messages
	return &resp, nil
}

func validateIntArg(i int, constraints api.ValueConstraints) bool {
	if constraints.Min != nil {
		if i < *constraints.Min {
			return false
		}
	}
	if constraints.Max != nil {
		if i > *constraints.Max {
			return false
		}
	}
	return true
}

func intConstraintString(constraints api.ValueConstraints) string {
	var min, max string
	if constraints.Min != nil {
		min = fmt.Sprintf("%d", *constraints.Min)
	}
	if constraints.Max != nil {
		max = fmt.Sprintf("%d", *constraints.Max)
	}
	return fmt.Sprintf("[%s,%s]", min, max)
}

func evaluateTemplate(resourceProvider yamlkit.ResourceProvider, functionContext *api.FunctionContext, valueTemplate string) (string, error) {
	f := template.FuncMap{}
	if resourceProvider != nil {
		f["normalizeName"] = resourceProvider.NormalizeName
	}
	f["toUpper"] = strings.ToUpper
	f["toLower"] = strings.ToLower
	f["toUpper"] = strings.ToUpper
	f["trimSpace"] = strings.TrimSpace
	f["trimSuffix"] = strings.TrimSuffix
	f["trimPrefix"] = strings.TrimPrefix
	tmpl, err := template.New("value").Funcs(f).Parse(valueTemplate)
	if err != nil {
		return "", errors.Wrap(err, fmt.Sprintf("couldn't parse template %s", valueTemplate))
	}
	var out bytes.Buffer
	err = tmpl.Execute(&out, functionContext)
	if err != nil {
		return "", errors.Wrap(err, fmt.Sprintf("couldn't evaluate template %s", valueTemplate))
	}
	return out.String(), nil
}

// TODO: Add built-in functions, as with templates
func evaluateCEL(resourceProvider yamlkit.ResourceProvider, functionContext *api.FunctionContext, valueExpression string) (string, error) {
	env, err := cel.NewEnv(
		cel.Variable("functionContext", cel.DynType),
	)
	if err != nil {
		return "", errors.Wrap(err, "failed to create CEL environment")
	}
	expr, issues := env.Compile(valueExpression)
	if issues != nil {
		return "", fmt.Errorf("failed to compile expression %s: %v", valueExpression, issues)
	}
	if !expr.OutputType().IsExactType(cel.StringType) {
		return "", fmt.Errorf("expression %s does not evaluate to a string", valueExpression)
	}
	program, err := env.Program(expr)
	if err != nil {
		return "", errors.Wrap(err, fmt.Sprintf("failed to create program for expression %s", valueExpression))
	}
	functionContextData, err := json.Marshal(functionContext)
	if err != nil {
		return "", errors.Wrap(err, "failed to marshal function context")
	}
	var functionContextMap map[string]any
	err = json.Unmarshal(functionContextData, &functionContextMap)
	if err != nil {
		return "", errors.Wrap(err, "failed to unmarshal function context")
	}
	obj := map[string]any{
		"functionContext": functionContextMap,
	}
	val, _, err := program.Eval(obj)
	if err != nil {
		return "", errors.Wrap(err, fmt.Sprintf("failed to evaluate program for expression %s", valueExpression))
	}
	nativeVal := val.Value()
	stringVal, ok := nativeVal.(string)
	if !ok {
		return "", fmt.Errorf("failed to convert result of expression %v to string", val.Value())
	}
	return stringVal, nil
}

// ValidateAndBuildArguments validates function arguments and builds an in-order argument list.
// It returns the validated arguments or an error if validation fails.
func ValidateAndBuildArguments(resourceProvider yamlkit.ResourceProvider, functionContext *api.FunctionContext, invocation *api.FunctionInvocation, f *api.FunctionSignature) ([]api.FunctionArgument, error) {
	nargs := len(invocation.Arguments)
	if nargs < f.RequiredParameters {
		usage := fmt.Sprintf("insufficient arguments: got %d, expected %d:", nargs, f.RequiredParameters)
		for _, arg := range f.Parameters {
			usage += fmt.Sprintf(" %s (reqd: %v),", arg.ParameterName, arg.Required)
		}
		return nil, errors.New(usage)
	}

	varargsParameterName := ""
	if f.VarArgs {
		varargsParameterName = f.Parameters[len(f.Parameters)-1].ParameterName
	} else {
		if nargs > len(f.Parameters) {
			usage := fmt.Sprintf("too many arguments: got %d, expected %d:", nargs, len(f.Parameters))
			for _, arg := range f.Parameters {
				usage += fmt.Sprintf(" %s (reqd: %v),", arg.ParameterName, arg.Required)
			}
			return nil, errors.New(usage)
		}
	}

	parameterMap := map[string]int{}
	for i, parameter := range f.Parameters {
		parameterMap[parameter.ParameterName] = i
	}

	isInOrder := true
	mustBeInOrder := false
	argumentMap := map[string]int{}
	for i, arg := range invocation.Arguments {
		parameterIndex := i
		if f.VarArgs && parameterIndex >= len(f.Parameters) {
			parameterIndex = len(f.Parameters) - 1
		}
		argumentName := arg.ParameterName
		if arg.ParameterName == "" {
			mustBeInOrder = true
			if isInOrder {
				parameterName := f.Parameters[parameterIndex].ParameterName
				_, present := argumentMap[parameterName]
				if present {
					if !f.VarArgs || parameterName != varargsParameterName {
						return nil, fmt.Errorf("argument %s is not a varargs parameter of function %s", parameterName, invocation.FunctionName)
					}
				} else {
					argumentName = parameterName
					invocation.Arguments[i].ParameterName = parameterName
					argumentMap[parameterName] = i
				}
			}
		} else {
			var ok bool
			parameterIndex, ok = parameterMap[argumentName]
			if !ok {
				return nil, fmt.Errorf("argument %s is not a valid parameter of function %s", arg.ParameterName, invocation.FunctionName)
			}
			if parameterIndex != i && (!f.VarArgs || i != len(f.Parameters)-1) {
				isInOrder = false
			}

			_, present := argumentMap[argumentName]
			if present {
				if f.VarArgs {
					if argumentName != varargsParameterName {
						return nil, fmt.Errorf("argument %s appears multiple times but is not the last argument of function %s", argumentName, invocation.FunctionName)
					}
				} else {
					return nil, fmt.Errorf("argument %s appears multiple times but function %s is not varargs", argumentName, invocation.FunctionName)
				}
			} else {
				argumentMap[argumentName] = i
			}
		}

		parameter := f.Parameters[parameterIndex]
		switch v := arg.Value.(type) {
		case string:
			if arg.Evaluator != "" {
				switch arg.Evaluator {
				case "template":
					renderedValue, err := evaluateTemplate(resourceProvider, functionContext, v)
					if err != nil {
						return nil, errors.Wrap(err, fmt.Sprintf("cannot evaluate template for parameter %s", parameter.ParameterName))
					}
					v = renderedValue
				case "cel":
					renderedValue, err := evaluateCEL(resourceProvider, functionContext, v)
					if err != nil {
						return nil, errors.Wrap(err, fmt.Sprintf("cannot evaluate CEL for parameter %s", parameter.ParameterName))
					}
					v = renderedValue
				default:
					return nil, errors.New("unsupported Evaluator " + arg.Evaluator)
				}
			}
			switch parameter.DataType {
			case api.DataTypeInt:
				intVal, err := strconv.Atoi(v)
				if err != nil {
					return nil, fmt.Errorf("cannot convert argument %s value %s to int: %w", argumentName, v, err)
				}
				invocation.Arguments[i].Value = intVal
				if !validateIntArg(intVal, parameter.ValueConstraints) {
					return nil, fmt.Errorf("argument %s value %d is out of range %s", argumentName, intVal, intConstraintString(parameter.ValueConstraints))
				}
			case api.DataTypeBool:
				boolVal, err := strconv.ParseBool(v)
				if err != nil {
					return nil, fmt.Errorf("cannot convert argument %s value %s to bool: %w", argumentName, v, err)
				}
				invocation.Arguments[i].Value = boolVal
			default:
				if !api.DataTypeIsSerializedAsString(parameter.DataType) {
					return nil, fmt.Errorf("argument %s data type %s is not of a string type", argumentName, parameter.DataType)
				} else {
					if parameter.ValueConstraints.Regexp != "" {
						r, err := regexp.Compile(parameter.ValueConstraints.Regexp)
						if err != nil {
							return nil, fmt.Errorf("validation regexp %s for argument %s is invalid", parameter.ValueConstraints.Regexp, argumentName)
						} else if !r.MatchString(v) {
							return nil, fmt.Errorf("value %s for argument %s failed to match %s", v, argumentName, parameter.ValueConstraints.Regexp)
						}
					} else if parameter.DataType == api.DataTypeEnum && len(parameter.ValueConstraints.EnumValues) > 0 {
						validValue := false
						for _, enumValue := range parameter.ValueConstraints.EnumValues {
							if v == enumValue {
								validValue = true
								break
							}
						}
						if !validValue {
							return nil, fmt.Errorf("value %s for argument %s is not in enum values %v", v, argumentName, parameter.ValueConstraints.EnumValues)
						}
					}
				}
				invocation.Arguments[i].Value = v
			}
		case float64:
			if parameter.DataType != api.DataTypeInt {
				return nil, fmt.Errorf("argument %s data type %s is not of type %s", argumentName, parameter.DataType, api.DataTypeInt)
			}
			invocation.Arguments[i].Value = int(v)
			if !validateIntArg(int(v), parameter.ValueConstraints) {
				return nil, fmt.Errorf("argument %s value %d is out of range %s", argumentName, int(v), intConstraintString(parameter.ValueConstraints))
			}
		case int:
			if parameter.DataType != api.DataTypeInt {
				return nil, fmt.Errorf("argument %s data type %s is not of type %s", argumentName, parameter.DataType, api.DataTypeInt)
			} else if !validateIntArg(v, parameter.ValueConstraints) {
				return nil, fmt.Errorf("argument %s value %d is out of range %s", argumentName, v, intConstraintString(parameter.ValueConstraints))
			}
		case bool:
			if parameter.DataType != api.DataTypeBool {
				return nil, fmt.Errorf("argument %s data type %s is not of type %s", argumentName, parameter.DataType, api.DataTypeBool)
			}
		default:
			return nil, fmt.Errorf("argument %s type %T is not of a supported type", argumentName, v)
		}
	}

	if !isInOrder {
		if mustBeInOrder {
			return nil, fmt.Errorf("mix of out-of-order named and unnamed arguments for function %s", invocation.FunctionName)
		}

		if f.RequiredParameters != len(f.Parameters) {
			for _, parameter := range f.Parameters {
				if parameter.Required {
					_, present := argumentMap[parameter.ParameterName]
					if !present {
						return nil, fmt.Errorf("missing required parameter %s for function %s", parameter.ParameterName, invocation.FunctionName)
					}
				}
			}
		}
	}

	var arguments []api.FunctionArgument
	if isInOrder {
		arguments = invocation.Arguments
	} else {
		for _, parameter := range f.Parameters {
			i, present := argumentMap[parameter.ParameterName]
			if present {
				arguments = append(arguments, invocation.Arguments[i])
			}
		}
		if f.VarArgs && len(arguments) < nargs {
			arguments = append(arguments, invocation.Arguments[len(arguments):]...)
		}
	}

	return arguments, nil
}

func (fh *FunctionHandler) ListCore() map[string]*FunctionRegistration {
	return fh.functionMap
}

func (fh *FunctionHandler) List(c echo.Context) error {
	// TODO: pagination
	return c.JSON(http.StatusOK, fh.functionMap) //nolint:wrapcheck // basic return
}

func (fh *FunctionHandler) ListPaths(c echo.Context) error {
	// TODO: pagination
	return c.JSON(http.StatusOK, fh.resourceProvider.GetPathRegistry()) //nolint:wrapcheck // basic return
}

func (fh *FunctionHandler) RegisterFunction(functionName string, registration *FunctionRegistration) error {
	// Allow registered functions to be cleared
	if registration == nil {
		delete(fh.functionMap, functionName)
		return nil
	}

	// Register a function
	numRequired := 0
	for _, parameter := range registration.Parameters {
		if parameter.Required {
			numRequired++
		}
	}
	registration.RequiredParameters = numRequired
	_, existing := fh.functionMap[functionName]
	if existing {
		return fmt.Errorf("function %s already registered", functionName)
	}
	if registration.OutputInfo != nil && registration.OutputInfo.OutputType == api.OutputTypeValidationResult {
		registration.Validating = true
	} else if registration.Validating {
		return fmt.Errorf("output type %s not valid for validating functions", string(registration.OutputInfo.OutputType))
	}
	fh.functionMap[functionName] = registration
	return nil
}

// FunctionImplementationArguments bundles all inputs to a function implementation.
type FunctionImplementationArguments struct {
	FunctionContext *api.FunctionContext
	Options         *api.FunctionOptions
	ParsedData      gaby.Container
	ParsedOtherData map[api.OtherDataSource]gaby.Container
	Arguments       []api.FunctionArgument
}

type FunctionImplementation func(args FunctionImplementationArguments) (gaby.Container, any, error)

type FunctionRegistration struct {
	api.FunctionSignature
	Function     FunctionImplementation `json:"-"` // implementation
	FunctionInit func() error           `json:"-"` // optional one-time initialization
}

// GetPathRegistry returns the path registry from the resource provider.
func (fh *FunctionHandler) GetPathRegistry() api.AttributeNameToResourceTypeToPathToVisitorInfoType {
	return fh.resourceProvider.GetPathRegistry()
}

// GetAttributes returns the list of registered attribute names from the path registry.
func (fh *FunctionHandler) GetAttributes() []api.AttributeName {
	pathRegistry := fh.resourceProvider.GetPathRegistry()
	attributes := make([]api.AttributeName, 0, len(pathRegistry))
	for attrName := range pathRegistry {
		attributes = append(attributes, attrName)
	}
	return attributes
}
