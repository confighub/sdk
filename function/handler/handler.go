// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"

	"github.com/cockroachdb/errors"
	"github.com/labstack/echo/v4"
	"github.com/labstack/gommon/log"

	"github.com/confighub/sdk/configkit"
	"github.com/confighub/sdk/function/api"
	"github.com/confighub/sdk/third_party/gaby"
)

// FunctionProvider defines the interface for a toolchain that registers functions.
type FunctionProvider interface {
	RegisterFunctions(fh FunctionRegistry)
	SetPathRegistry(fh FunctionRegistry)
	GetToolchainPath() string
}

// FunctionRegistry defines the interface for registering functions.
// This allows decoupling internal packages from the concrete FunctionHandler implementation.
type FunctionRegistry interface {
	RegisterFunction(functionName string, registration *FunctionRegistration) error
	GetHandlerImplementation(functionName string) FunctionImplementation
	SetPathRegistry(pathRegistry api.AttributeNameToResourceTypeToPathToVisitorInfoType)
	SetConverter(converter configkit.ConfigConverter)
	GetConverter() configkit.ConfigConverter
}

type FunctionHandler struct {
	functionMap  map[string]*FunctionRegistration
	pathRegistry api.AttributeNameToResourceTypeToPathToVisitorInfoType
	converter    configkit.ConfigConverter
}

// Ensure FunctionHandler implements FunctionRegistry
var _ FunctionRegistry = (*FunctionHandler)(nil)

func NewFunctionHandler() *FunctionHandler {
	fh := &FunctionHandler{}
	fh.functionMap = make(map[string]*FunctionRegistration)
	fh.pathRegistry = make(api.AttributeNameToResourceTypeToPathToVisitorInfoType)
	return fh
}

func (fh *FunctionHandler) GetHandlerImplementation(functionName string) FunctionImplementation {
	registration, ok := fh.functionMap[functionName]
	if !ok {
		return nil
	}
	return registration.Function
}

func (fh *FunctionHandler) SetConverter(converter configkit.ConfigConverter) {
	fh.converter = converter
}

func (fh *FunctionHandler) GetConverter() configkit.ConfigConverter {
	return fh.converter
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

	// Convert to YAML
	yamlData, err := fh.GetConverter().NativeToYAML(functionInvocation.ConfigData)
	if err != nil {
		return nil, err
	}
	serializedData := yamlData

	// Errors below are not wrapped here. They need to be wrapped at origin, if necessary.
	// The reason is so that we can return detailed error messages.
	success := true
	numFilters := functionInvocation.NumFilters
	messages := []string{}
	mutations := []api.ResourceMutation{}
	mutators := []int{}
	var output any
	var outputType api.OutputType
	for functionIndex, invocation := range functionInvocation.FunctionInvocations {
		invalid := false
		f, existed := fh.functionMap[invocation.FunctionName]
		if !existed {
			invocationInfo := fmt.Sprintf("invoke %s: function does not exist", invocation.FunctionName)
			log.Info(invocationInfo)
			messages = append(messages, invocationInfo)
			invalid = true
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
		for _, arg := range invocation.Arguments {
			invocationInfo += fmt.Sprintf(" %v", arg.Value)
		}

		// TODO: Factor out argument validation so that we can use it in cub

		nargs := len(invocation.Arguments)
		if nargs < f.RequiredParameters {
			invocationInfo += ": insufficient arguments"
			log.Info(invocationInfo)
			usage := fmt.Sprintf("insufficient arguments: got %d, expected %d:", nargs, f.RequiredParameters)
			for _, arg := range f.Parameters {
				usage += fmt.Sprintf(" %s (reqd: %v),", arg.ParameterName, arg.Required)
			}
			messages = append(messages, usage)
			invalid = true
			success = false
			if functionInvocation.StopOnError {
				break
			}
		}

		varargsParameterName := ""
		if f.VarArgs {
			varargsParameterName = f.Parameters[len(f.Parameters)-1].ParameterName
		} else {
			if nargs > len(f.Parameters) {
				invocationInfo += ": too many arguments"
				log.Info(invocationInfo)
				usage := fmt.Sprintf("too many arguments: got %d, expected %d:", nargs, len(f.Parameters))
				for _, arg := range f.Parameters {
					usage += fmt.Sprintf(" %s (reqd: %v),", arg.ParameterName, arg.Required)
				}
				messages = append(messages, usage)
				invalid = true
				success = false
				if functionInvocation.StopOnError {
					break
				} else {
					continue
				}
			}
		}

		parameterMap := map[string]int{}
		for i, parameter := range f.Parameters {
			parameterMap[parameter.ParameterName] = i
		}

		// TODO: check that once we've seen the first vararg value, no other arguments appear afterward

		isInOrder := true
		mustBeInOrder := false
		argumentMap := map[string]int{}
	argLoop:
		for i, arg := range invocation.Arguments {
			parameterIndex := i
			if f.VarArgs && parameterIndex >= len(f.Parameters) {
				parameterIndex = len(f.Parameters) - 1
			}
			argumentName := arg.ParameterName
			if arg.ParameterName == "" {
				// If any argument does not include the parameter name, they must occur in order
				mustBeInOrder = true
				// If the arguments are not in order already, that will trigger an error.
				// If they are, add this argument to the arg map. It should not have appeared already,
				// unless it's the last parameter of a varargs function.
				if isInOrder {
					parameterName := f.Parameters[parameterIndex].ParameterName
					_, present := argumentMap[parameterName]
					if present {
						if !f.VarArgs || parameterName != varargsParameterName {
							// This really shouldn't happen
							invocationInfo += ": repeated parameter"
							message := fmt.Sprintf("argument %s is not a varargs parameter of function %s", parameterName, invocation.FunctionName)
							messages = append(messages, message)
							invalid = true
							break argLoop
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
					invocationInfo += ": invalid parameter"
					message := fmt.Sprintf("argument %s is not a valid parameter of function %s", arg.ParameterName, invocation.FunctionName)
					messages = append(messages, message)
					invalid = true
					break argLoop
				}
				if parameterIndex != i && (!f.VarArgs || i != len(f.Parameters)-1) {
					isInOrder = false
				}

				_, present := argumentMap[argumentName]
				if present {
					if f.VarArgs {
						if argumentName != varargsParameterName {
							invocationInfo += ": repeated parameter"
							message := fmt.Sprintf("argument %s appears multiple times but is not the last argument of function %s", argumentName, invocation.FunctionName)
							messages = append(messages, message)
							invalid = true
							break argLoop
						}
					} else {
						invocationInfo += ": repeated parameter"
						message := fmt.Sprintf("argument %s appears multiple times but function %s is not varargs", argumentName, invocation.FunctionName)
						messages = append(messages, message)
						invalid = true
						break argLoop
					}
				} else {
					argumentMap[argumentName] = i
				}
			}

			// Verify that the argument value type is correct, or cast them if requested
			parameter := f.Parameters[parameterIndex]
			switch v := arg.Value.(type) {
			case string:
				switch parameter.DataType {
				case api.DataTypeInt:
					if functionInvocation.CastStringArgsToScalars {
						intVal, err := strconv.Atoi(v)
						if err != nil {
							// TODO: improve the error message
							invalid = true
						}
						invocation.Arguments[i].Value = intVal
						if !validateIntArg(intVal, parameter.ValueConstraints) {
							invocationInfo += ": argument value out of range"
							message := fmt.Sprintf("argument %s value %d is out of range %s", argumentName, intVal, intConstraintString(parameter.ValueConstraints))
							messages = append(messages, message)
							invalid = true
							break argLoop
						}
					} else {
						invalid = true
					}
				case api.DataTypeBool:
					if functionInvocation.CastStringArgsToScalars {
						boolVal, err := strconv.ParseBool(v)
						if err != nil {
							// TODO: improve the error message
							invalid = true
						}
						invocation.Arguments[i].Value = boolVal
					} else {
						invalid = true
					}
				default:
					if !api.DataTypeIsSerializedAsString(parameter.DataType) {
						invalid = true
					} else {
						if parameter.ValueConstraints.Regexp != "" {
							r, err := regexp.Compile(parameter.ValueConstraints.Regexp)
							if err != nil {
								message := fmt.Sprintf("validation regexp %s for argument %s is invalid", parameter.ValueConstraints.Regexp, argumentName)
								messages = append(messages, message)
								// not fatal
							} else if !r.MatchString(v) {
								invocationInfo += ": value failed validation"
								message := fmt.Sprintf("value %s for argument %s failed to match %s", v, argumentName, parameter.ValueConstraints.Regexp)
								messages = append(messages, message)
								invalid = true
								break argLoop
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
								invocationInfo += ": value not in enum"
								message := fmt.Sprintf("value %s for argument %s is not in enum values %v", v, argumentName, parameter.ValueConstraints.EnumValues)
								messages = append(messages, message)
								invalid = true
								break argLoop
							}
						}
					}
				}

				if invalid {
					invocationInfo += ": invalid argument type"
					message := fmt.Sprintf("argument %s data type %s is not of a string type", argumentName, parameter.DataType)
					messages = append(messages, message)
					break argLoop
				}
			// Integers are "Numbers" in JSON, which are deserialized as float64 in Go.
			// We treat all numbers as integers currently. We can't fallthrough in a type switch.
			case float64:
				if parameter.DataType != api.DataTypeInt {
					invocationInfo += ": invalid argument type"
					message := fmt.Sprintf("argument %s data type %s is not of type %s", argumentName, parameter.DataType, api.DataTypeInt)
					messages = append(messages, message)
					invalid = true
					break argLoop
				}
				invocation.Arguments[i].Value = int(v)
				if !validateIntArg(int(v), parameter.ValueConstraints) {
					invocationInfo += ": argument value out of range"
					message := fmt.Sprintf("argument %s value %d is out of range %s", argumentName, int(v), intConstraintString(parameter.ValueConstraints))
					messages = append(messages, message)
					invalid = true
					break argLoop
				}
			case int:
				if parameter.DataType != api.DataTypeInt {
					invocationInfo += ": invalid argument type"
					message := fmt.Sprintf("argument %s data type %s is not of type %s", argumentName, parameter.DataType, api.DataTypeInt)
					messages = append(messages, message)
					invalid = true
					break argLoop
				} else if !validateIntArg(v, parameter.ValueConstraints) {
					invocationInfo += ": argument value out of range"
					message := fmt.Sprintf("argument %s value %d is out of range %s", argumentName, v, intConstraintString(parameter.ValueConstraints))
					messages = append(messages, message)
					invalid = true
					break argLoop
				}
			case bool:
				if parameter.DataType != api.DataTypeBool {
					invocationInfo += ": invalid argument type"
					message := fmt.Sprintf("argument %s data type %s is not of type %s", argumentName, parameter.DataType, api.DataTypeBool)
					messages = append(messages, message)
					invalid = true
					break argLoop
				}
			default:
				invocationInfo += ": invalid argument type"
				message := fmt.Sprintf("argument %s type %T is not of a supported type", argumentName, v)
				messages = append(messages, message)
				invalid = true
				break argLoop
			}
		}
		if !isInOrder {
			if mustBeInOrder {
				invocationInfo += ": mix of out-of-order named and unnamed arguments"
				message := fmt.Sprintf("mix of out-of-order named and unnamed arguments for function %s", invocation.FunctionName)
				messages = append(messages, message)
				invalid = true
			}

			// Check that all required parameters are present
			if f.RequiredParameters != len(f.Parameters) {
				for _, parameter := range f.Parameters {
					if parameter.Required {
						_, present := argumentMap[parameter.ParameterName]
						if !present {
							invocationInfo += ": missing required parameter"
							message := fmt.Sprintf("missing required parameter %s for function %s", parameter.ParameterName, invocation.FunctionName)
							messages = append(messages, message)
							invalid = true
						}
					}
				}
			}
		}

		if invalid {
			log.Info(invocationInfo)
			success = false
			if functionInvocation.StopOnError {
				break
			} else {
				continue
			}
		}

		// Build in-order argument list
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
				// The tail of the argument list should all be varargs
				arguments = append(arguments, invocation.Arguments[len(arguments):]...)
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
		newParsedData, functionOutput, err = f.Function(&functionContext, newParsedData, arguments, functionInvocation.LiveState)
		if err == nil && isFilter {
			validationResult, ok := functionOutput.(api.ValidationResult)
			if !ok {
				err = errors.New("validating functions must return type ValidationResult")
			} else if !validationResult.Passed {
				invocationInfo += ": " + "did not pass"
				log.Info(invocationInfo)
				break
			}
		}
		if err != nil {
			invocationInfo += ": " + err.Error()
			log.Info(invocationInfo)
			messages = append(messages, err.Error())
			success = false
			if functionInvocation.StopOnError {
				break
			}
		}

		invocationInfo += ": succeeded"
		log.Info(invocationInfo)

		if f.Mutating {
			newSerializedDataString := newParsedData.String()
			newSerializedData := []byte(newSerializedDataString)
			if !bytes.Equal(serializedData, newSerializedData) {
				computeMutationArguments := []api.FunctionArgument{
					{Value: string(serializedData)},
					{Value: functionIndex},
					{Value: true}, // already converted to YAML
				}
				_, newMutationsOutput, err := computeMutations.Function(&functionContext, newParsedData, computeMutationArguments, functionInvocation.LiveState)
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
				mutations = api.AddMutations(mutations, newMutations)
				mutators = append(mutators, functionIndex)
				serializedData = newSerializedData
			}
		}

		if functionOutput != nil && f.OutputInfo != nil {
			if output == nil {
				outputType = f.OutputInfo.OutputType
			}

			output, messages = api.CombineOutputs(
				f.FunctionName,
				functionContext.InstanceString(),
				outputType,
				f.OutputInfo.OutputType,
				output,
				functionOutput,
				functionInvocation.CombineValidationResults,
				functionIndex,
				messages,
			)
		}
	}

	var resp api.FunctionInvocationResponse
	resp.OrganizationID = functionInvocation.FunctionContext.OrganizationID
	resp.SpaceID = functionInvocation.FunctionContext.SpaceID
	resp.UnitID = functionInvocation.FunctionContext.UnitID
	resp.RevisionID = functionInvocation.FunctionContext.RevisionID

	// Convert from YAML back to the original format
	nativeData, err := fh.GetConverter().YAMLToNative(serializedData)
	// TODO: Handle this better
	if err != nil {
		return nil, err
	}
	resp.ConfigData = nativeData

	encodedOutput, err := json.Marshal(output)
	if err != nil {
		messages = append(messages, err.Error())
	}
	resp.Output = encodedOutput
	resp.OutputType = outputType
	resp.Success = success
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

func (fh *FunctionHandler) ListCore() map[string]*FunctionRegistration {
	return fh.functionMap
}

func (fh *FunctionHandler) List(c echo.Context) error {
	// TODO: pagination
	return c.JSON(http.StatusOK, fh.functionMap) //nolint:wrapcheck // basic return
}

func (fh *FunctionHandler) ListPaths(c echo.Context) error {
	// TODO: pagination
	return c.JSON(http.StatusOK, fh.pathRegistry) //nolint:wrapcheck // basic return
}

func (fh *FunctionHandler) RegisterFunction(functionName string, registration *FunctionRegistration) error {
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

// TODO: Put the function arguments into a struct so that it's extensible

type FunctionImplementation func(*api.FunctionContext, gaby.Container, []api.FunctionArgument, []byte) (gaby.Container, any, error)

type FunctionRegistration struct {
	api.FunctionSignature
	Function FunctionImplementation `json:"-"` // implementation
}

// SetPathRegistry sets the path registry.
func (fh *FunctionHandler) SetPathRegistry(pathRegistry api.AttributeNameToResourceTypeToPathToVisitorInfoType) {
	fh.pathRegistry = pathRegistry
}
