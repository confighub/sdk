// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package generic

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/confighub/sdk/core/configkit"
	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/function/handler"
	"github.com/confighub/sdk/core/third_party/gaby"
	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/ext"
	"sigs.k8s.io/yaml"
)

// functionContextToMap converts a FunctionContext into a generic map keyed by
// the JSON field names (e.g. "TargetID", "UnitSlug") so that ConfigHub metadata
// can be exposed to interpreted function languages (Starlark, CEL). It returns
// an empty map when functionContext is nil.
func functionContextToMap(functionContext *api.FunctionContext) (map[string]any, error) {
	if functionContext == nil {
		return map[string]any{}, nil
	}
	data, err := json.Marshal(functionContext)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal function context: %w", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("failed to unmarshal function context: %w", err)
	}
	return m, nil
}

// NewCELEnv creates a CEL environment with the given variables and optional extra options.
// This provides the base CEL environment used by all generic CEL functions.
func NewCELEnv(vars []cel.EnvOption, extraOpts ...cel.EnvOption) (*cel.Env, error) {
	opts := []cel.EnvOption{
		cel.DefaultUTCTimeZone(true),
		cel.CrossTypeNumericComparisons(true),
		cel.OptionalTypes(),
		ext.Strings(),
		ext.Sets(),
		ext.Lists(),
		ext.Encoders(),
		celSelectsFunction(),
		celPodLabelsFunction(),
		celNamespaceLabelsFunction(),
	}
	opts = append(opts, vars...)
	opts = append(opts, extraOpts...)
	return cel.NewEnv(opts...)
}

// celSelectsFunction is selects(selector, labels): whether a Kubernetes label selector selects the
// labels, as LabelSelectorMatches reads them, so a rule can say which NetworkPolicies select a
// workload's pods without spelling out matchExpressions.
func celSelectsFunction() cel.EnvOption {
	return cel.Function("selects",
		cel.Overload("selects_dyn_dyn", []*cel.Type{cel.DynType, cel.DynType}, cel.BoolType,
			cel.BinaryBinding(func(selector, labelValues ref.Val) ref.Val {
				matched, err := LabelSelectorMatches(CelToGo(selector), CelToGo(labelValues))
				if err != nil {
					return types.NewErr("selects: %v", err)
				}
				return types.Bool(matched)
			})))
}

// celPodLabelsFunction is podLabels(resource): the labels of the pods a resource runs, as PodLabels
// reads them, null for one that runs none, so that a rule matching selectors against workloads
// does not spell out where each kind keeps its pod template.
func celPodLabelsFunction() cel.EnvOption {
	return cel.Function("podLabels",
		cel.Overload("podLabels_dyn", []*cel.Type{cel.DynType}, cel.DynType,
			cel.UnaryBinding(func(resource ref.Val) ref.Val {
				return types.DefaultTypeAdapter.NativeToValue(PodLabels(CelToGo(resource)))
			})))
}

// celNamespaceLabelsFunction is namespaceLabels(namespaces, name): the labels of the Namespace
// named name among a list of Namespaces, as a namespaceSelector is matched against them
// (NamespaceLabels).
func celNamespaceLabelsFunction() cel.EnvOption {
	return cel.Function("namespaceLabels",
		cel.Overload("namespaceLabels_dyn_string", []*cel.Type{cel.DynType, cel.StringType}, cel.DynType,
			cel.BinaryBinding(func(namespaces, name ref.Val) ref.Val {
				nameString, ok := name.Value().(string)
				if !ok {
					return types.NewErr("namespaceLabels: the name must be a string")
				}
				return types.DefaultTypeAdapter.NativeToValue(NamespaceLabels(CelToGo(namespaces), nameString))
			})))
}

// CelParseParams extracts key=value params from function arguments starting at the given index
// and returns them as a map[string]any suitable for CEL evaluation.
func CelParseParams(args []api.FunctionArgument, startIndex int) (map[string]any, error) {
	params := make(map[string]any, len(args)-startIndex)
	for i := startIndex; i < len(args); i++ {
		s, ok := args[i].Value.(string)
		if !ok {
			return nil, fmt.Errorf("param argument %d must be a string, got %T", i, args[i].Value)
		}
		key, value, found := strings.Cut(s, "=")
		if !found {
			return nil, fmt.Errorf("param argument %d must be in key=value format, got %q", i, s)
		}
		params[key] = value
	}
	return params, nil
}

func registerVetCELExpr(fh handler.FunctionRegistry, converter configkit.ConfigConverter, resourceProvider yamlkit.ResourceProvider) {
	if err := fh.RegisterFunction("vet-celexpr", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "vet-celexpr",
			Parameters: []api.FunctionParameter{
				{
					ParameterName: "validation-expr",
					Required:      true,
					Description:   "CEL (Common Expression Language) expression to validate each resource. The current resource is referenced with the prefix 'r.' See https://cel.dev/ for language details.",
					DataType:      api.DataTypeCEL,
					// TODO: Override this with ToolchainType-specific examples.
					Example: "r.kind != 'Deployment' || (r.spec.template.spec.securityContext.runAsNonRoot == true && r.spec.template.spec.containers.all(container, !has(container.securityContext.runAsNonRoot) || container.securityContext.runAsNonRoot == true)) || r.spec.template.spec.containers.all(container, has(container.securityContext.runAsNonRoot) && container.securityContext.runAsNonRoot == true)",
				},
			},
			OutputInfo: &api.FunctionOutput{
				ResultName:  "passed",
				Description: "True if validation passed, false otherwise",
				OutputType:  api.OutputTypeValidationResult,
				Schema:      &api.ValidationResultListSchema,
			},
			Mutating:              false,
			Validating:            true,
			Hermetic:              true,
			Idempotent:            true,
			Description:           `Fails each resource for which the validation expression is false. Fix: change the resource so the expression holds; the Trigger's Description says what it requires.`,
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
			return genericFnCELValidate(resourceProvider, fArgs.Options, fArgs.FunctionContext, fArgs.ParsedData, fArgs.Arguments)
		},
	}); err != nil {
		slog.Error("failed to register function", "error", err)
	}
}

// TODO: Deprecated in favor of vet-celexpr. Remove this.
func registerCELValidate(fh handler.FunctionRegistry, converter configkit.ConfigConverter, resourceProvider yamlkit.ResourceProvider) {
	if err := fh.RegisterFunction("cel-validate", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "cel-validate",
			Parameters: []api.FunctionParameter{
				{
					ParameterName: "validation-expr",
					Required:      true,
					Description:   "CEL (Common Expression Language) expression to validate each resource. The current resource is refenced with the prefix 'r.' See https://cel.dev/ for language details.",
					DataType:      api.DataTypeCEL,
					// TODO: Override this with ToolchainType-specific examples.
					Example: "r.kind != 'Deployment' || r.spec.template.spec.containers.all(container, container.securityContext.runAsNonRoot == true)",
				},
			},
			OutputInfo: &api.FunctionOutput{
				ResultName:  "passed",
				Description: "True if validation passed, false otherwise",
				OutputType:  api.OutputTypeValidationResult,
				Schema:      &api.ValidationResultListSchema,
			},
			Mutating:              false,
			Validating:            true,
			Hermetic:              true,
			Idempotent:            true,
			Description:           `[Deprecated; use vet-celexpr instead] Returns true if validation expression evaluates to true for all resources`,
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
			return genericFnCELValidate(resourceProvider, fArgs.Options, fArgs.FunctionContext, fArgs.ParsedData, fArgs.Arguments)
		},
	}); err != nil {
		slog.Error("failed to register function", "error", err)
	}
}

// CELVarOpts returns the standard CEL variable options for r, object, params,
// and functionContext (the ConfigHub FunctionContext metadata).
func CELVarOpts() []cel.EnvOption {
	return []cel.EnvOption{
		cel.Variable("r", cel.DynType),
		cel.Variable("object", cel.DynType),
		cel.Variable("params", cel.DynType),
		cel.Variable("functionContext", cel.DynType),
	}
}

// CELOtherDataVarOpts declares other and otherOrigins, the OtherData a function was passed
// (OtherResources): other.policies is the list of resources passed under the key policies, and
// otherOrigins.policies[i] where other.policies[i] came from.
func CELOtherDataVarOpts() []cel.EnvOption {
	return []cel.EnvOption{
		cel.Variable("other", cel.MapType(cel.StringType, cel.ListType(cel.DynType))),
		cel.Variable("otherOrigins", cel.MapType(cel.StringType, cel.ListType(cel.DynType))),
	}
}

// CELActivation builds the activation map for a resource with params and the
// FunctionContext metadata, which exposes fields such as
// functionContext.TargetID and functionContext.UnitSlug.
func CELActivation(dataMap map[string]any, params map[string]any, functionContext map[string]any) map[string]any {
	return map[string]any{
		"r":               dataMap,
		"object":          dataMap,
		"params":          params,
		"functionContext": functionContext,
	}
}

// registerVetCEL registers the vet-cel validation function that returns ValidationResult.
func registerVetCEL(fh handler.FunctionRegistry, converter configkit.ConfigConverter, resourceProvider yamlkit.ResourceProvider) {
	if err := fh.RegisterFunction("vet-cel", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "vet-cel",
			Parameters: []api.FunctionParameter{
				{
					ParameterName: "expression",
					Required:      true,
					Description:   "CEL expression to validate each resource. The current resource is available as 'r' (alias 'object'). Must return a bool (true=pass), or a map with 'passed' (bool), 'details' (list of strings), and optionally 'issues' (list of maps with Identifier and Message, naming the check each finding fails) and 'failed_attributes' (list of maps with ResourceName, ResourceType, Path, Value, and optionally Score and Issues). The 'params' dict and 'functionContext' map (ConfigHub metadata, e.g. functionContext.TargetID) are also available. Other Units passed as OtherData, such as those a Trigger's OtherUnitSelections select, are available as 'other', a map from each key to the list of resources passed under it, and where each came from as 'otherOrigins', at the same positions (UnitSlug, SpaceSlug, UnitID, SpaceID, RevisionNum, UnitLabels). selects(selector, labels) says whether a Kubernetes label selector selects the labels, podLabels(resource) gives the labels of the pods a resource runs, wherever its kind keeps them (null for one that runs none), and namespaceLabels(namespaces, name) the labels of a Namespace in a list as a namespaceSelector matches them, e.g. other.policies.exists(p, p.metadata.namespace == r.metadata.namespace && selects(p.spec.podSelector, podLabels(r))).",
					DataType:      api.DataTypeCEL,
					Example:       `r.kind != 'Deployment' || r.spec.replicas <= 10`,
				},
				{
					ParameterName: "param",
					Required:      false,
					Description:   "Parameters passed as key=value strings, accessible via the 'params' dict",
					DataType:      api.DataTypeString,
				},
			},
			VarArgs: true,
			OutputInfo: &api.FunctionOutput{
				ResultName:  "validation",
				Description: "Validation result from CEL expression",
				OutputType:  api.OutputTypeValidationResult,
				Schema:      &api.ValidationResultListSchema,
			},
			Mutating:              false,
			Validating:            true,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Fails each resource for which a CEL expression, evaluated once per resource, returns false or {passed: false, details: [string]}. The expression can read other Units passed as OtherData, as other. Fix: change the resource so the expression holds; its details, or the Trigger's Description, say what it requires.",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
			OtherDataAnyKey:       true,
		},
		Function: func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
			other, err := OtherResourcesOf(fArgs)
			if err != nil {
				return fArgs.ParsedData, api.ValidationResultFalse, err
			}
			return GenericFnVetCELWithOtherData(resourceProvider, fArgs.Options, fArgs.FunctionContext, fArgs.ParsedData, other, fArgs.Arguments)
		},
	}); err != nil {
		slog.Error("failed to register function", "error", err)
	}
}

// GenericFnVetCEL implements vet-cel validation. Extra CEL env options can be passed
// by toolchain-specific overrides (e.g., Kubernetes admission policy libraries).
func GenericFnVetCEL(resourceProvider yamlkit.ResourceProvider, options *api.FunctionOptions, functionContext *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, extraEnvOpts ...cel.EnvOption) (gaby.Container, any, error) {
	return GenericFnVetCELWithOtherData(resourceProvider, options, functionContext, parsedData, OtherResources{}, args, extraEnvOpts...)
}

// GenericFnVetCELWithOtherData is GenericFnVetCEL with the OtherData the function was passed,
// which the expression reads as other and otherOrigins.
func GenericFnVetCELWithOtherData(resourceProvider yamlkit.ResourceProvider, options *api.FunctionOptions, functionContext *api.FunctionContext, parsedData gaby.Container, other OtherResources, args []api.FunctionArgument, extraEnvOpts ...cel.EnvOption) (gaby.Container, any, error) {
	expression := args[0].Value.(string)

	params, err := CelParseParams(args, 1)
	if err != nil {
		return parsedData, api.ValidationResultFalse, err
	}

	functionContextMap, err := functionContextToMap(functionContext)
	if err != nil {
		return parsedData, api.ValidationResultFalse, err
	}

	env, err := NewCELEnv(append(CELVarOpts(), CELOtherDataVarOpts()...), extraEnvOpts...)
	if err != nil {
		return parsedData, api.ValidationResultFalse, fmt.Errorf("failed to create CEL environment: %v", err)
	}
	otherResources, otherOrigins := other.Resources, other.Origins
	if otherResources == nil {
		otherResources, otherOrigins = map[string][]any{}, map[string][]any{}
	}

	ast, issues := env.Compile(expression)
	if issues != nil {
		failedResult := api.ValidationResultFalse
		failedResult.Details = []string{fmt.Sprintf("failed to compile expression: %v", issues)}
		return parsedData, failedResult, nil
	}

	program, err := env.Program(ast)
	if err != nil {
		return parsedData, api.ValidationResultFalse, fmt.Errorf("failed to create program: %v", err)
	}

	overallResult := api.ValidationResult{Passed: true}

	_, err = yamlkit.VisitResourcesFiltered(parsedData, nil, resourceProvider, options, func(doc *gaby.YamlDoc, output any, index int, resourceInfo *api.ResourceInfo) (any, []error) {
		var dataMap map[string]any
		if err := yaml.Unmarshal(doc.Bytes(), &dataMap); err != nil {
			return output, []error{err}
		}

		activation := CELActivation(dataMap, params, functionContextMap)
		activation["other"] = otherResources
		activation["otherOrigins"] = otherOrigins
		val, _, err := program.Eval(activation)
		if err != nil {
			overallResult.Passed = false
			overallResult.Details = append(overallResult.Details, fmt.Sprintf("CEL evaluation error on resource %s: %v", resourceInfo.ResourceName, err))
			return output, nil
		}

		vr := parseCELValidationResult(val, resourceInfo)
		if !vr.Passed {
			overallResult.Passed = false
		}
		overallResult.Details = append(overallResult.Details, vr.Details...)
		overallResult.Issues = append(overallResult.Issues, vr.Issues...)
		overallResult.FailedAttributes = append(overallResult.FailedAttributes, vr.FailedAttributes...)
		overallResult.MaxScore = api.ScoreMax(overallResult.MaxScore, vr.MaxScore)
		return output, nil
	})
	if err != nil {
		return parsedData, api.ValidationResultFalse, err
	}

	return parsedData, overallResult, nil
}

// parseCELValidationResult converts a CEL result value to a ValidationResult.
// Accepts bool or map with "passed", "details", "issues", "failed_attributes" keys.
// An issue is a map with Identifier and Message, which is how one expression reports
// several checks: each finding names the check it fails. A failed attribute may carry
// its own Issues, and a Score, the highest of which is the result's MaxScore.
func parseCELValidationResult(val ref.Val, resourceInfo *api.ResourceInfo) api.ValidationResult {
	goVal := CelToGo(val)

	// If bool, simple pass/fail
	if b, ok := goVal.(bool); ok {
		vr := api.ValidationResult{Passed: b}
		if !b {
			vr.Details = []string{fmt.Sprintf("resource %s failed validation", resourceInfo.ResourceName)}
		}
		return vr
	}

	// If map, extract structured result
	m, ok := goVal.(map[string]any)
	if !ok {
		return api.ValidationResult{
			Passed:  false,
			Details: []string{fmt.Sprintf("CEL expression must return bool or map, got %T", goVal)},
		}
	}

	vr := api.ValidationResult{Passed: true}
	if passed, ok := m["passed"]; ok {
		if b, ok := passed.(bool); ok {
			vr.Passed = b
		}
	}

	if details, ok := m["details"]; ok {
		if detailList, ok := details.([]any); ok {
			for _, d := range detailList {
				vr.Details = append(vr.Details, fmt.Sprintf("%v", d))
			}
		}
	}

	vr.Issues = celIssues(m["issues"])

	if failedAttrs, ok := m["failed_attributes"]; ok {
		if attrList, ok := failedAttrs.([]any); ok {
			for _, attr := range attrList {
				if attrMap, ok := attr.(map[string]any); ok {
					av := celMapToAttributeValue(attrMap)
					vr.FailedAttributes = append(vr.FailedAttributes, av)
					vr.MaxScore = api.ScoreMax(vr.MaxScore, av.Score)
				}
			}
		}
	}

	return vr
}

// celIssues converts a list of maps with Identifier and Message to Issues. Anything else
// in the list is not an issue, and is left out.
func celIssues(v any) []api.Issue {
	list, ok := v.([]any)
	if !ok {
		return nil
	}
	var issues []api.Issue
	for _, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		var issue api.Issue
		if id, ok := m["Identifier"]; ok {
			issue.Identifier = fmt.Sprintf("%v", id)
		}
		if msg, ok := m["Message"]; ok {
			issue.Message = fmt.Sprintf("%v", msg)
		}
		if issue.Identifier != "" || issue.Message != "" {
			issues = append(issues, issue)
		}
	}
	return issues
}

func celMapToAttributeValue(m map[string]any) api.AttributeValue {
	av := api.AttributeValue{}
	if v, ok := m["ResourceName"]; ok {
		av.ResourceName = api.ResourceName(fmt.Sprintf("%v", v))
	}
	if v, ok := m["ResourceType"]; ok {
		av.ResourceType = api.ResourceType(fmt.Sprintf("%v", v))
	}
	if v, ok := m["Path"]; ok {
		av.Path = api.ResolvedPath(fmt.Sprintf("%v", v))
	}
	if v, ok := m["Value"]; ok {
		av.Value = v
	}
	if v, ok := m["AttributeName"]; ok {
		av.AttributeName = api.AttributeName(fmt.Sprintf("%v", v))
	}
	if v, ok := m["DataType"]; ok {
		av.DataType = api.DataType(fmt.Sprintf("%v", v))
	}
	if v, ok := m["Score"]; ok {
		// A score that is not one of the names is no score, rather than a failure of the
		// whole result: the finding still stands.
		if score, err := api.ParseScore(fmt.Sprintf("%v", v)); err == nil {
			av.Score = score
		}
	}
	av.Issues = celIssues(m["Issues"])
	return av
}

// registerGetCEL registers the get-cel extraction function.
func registerGetCEL(fh handler.FunctionRegistry, converter configkit.ConfigConverter, resourceProvider yamlkit.ResourceProvider) {
	if err := fh.RegisterFunction("get-cel", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "get-cel",
			Parameters: []api.FunctionParameter{
				{
					ParameterName: "expression",
					Required:      true,
					Description:   "CEL expression that extracts values from a resource. The current resource is available as 'r' (alias 'object'). Must return a list of maps, each with fields: ResourceName (string), ResourceType (string), Path (string), Value (any), and optionally AttributeName (string), DataType (string). The 'params' dict and 'functionContext' map (ConfigHub metadata, e.g. functionContext.TargetID) are also available.",
					DataType:      api.DataTypeCEL,
					Example:       `r.kind == 'Deployment' ? [{"ResourceName": r.metadata.?namespace.orValue("") + "/" + r.metadata.name, "ResourceType": r.apiVersion + "/" + r.kind, "Path": "spec.replicas", "Value": r.spec.replicas}] : []`,
				},
				{
					ParameterName: "param",
					Required:      false,
					Description:   "Parameters passed as key=value strings, accessible via the 'params' dict",
					DataType:      api.DataTypeString,
				},
			},
			VarArgs: true,
			OutputInfo: &api.FunctionOutput{
				ResultName:  "attributes",
				Description: "Extracted attribute values from CEL expression",
				OutputType:  api.OutputTypeAttributeValueList,
				Schema:      &api.AttributeValueListSchema,
			},
			Mutating:              false,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Extracts attribute values from configuration resources using a CEL expression that returns a list of attribute value maps. Evaluated once per resource.",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
			return GenericFnGetCEL(resourceProvider, fArgs.Options, fArgs.FunctionContext, fArgs.ParsedData, fArgs.Arguments)
		},
	}); err != nil {
		slog.Error("failed to register function", "error", err)
	}
}

// GenericFnGetCEL implements get-cel extraction. Extra CEL env options can be passed
// by toolchain-specific overrides.
func GenericFnGetCEL(resourceProvider yamlkit.ResourceProvider, options *api.FunctionOptions, functionContext *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, extraEnvOpts ...cel.EnvOption) (gaby.Container, any, error) {
	expression := args[0].Value.(string)

	params, err := CelParseParams(args, 1)
	if err != nil {
		return parsedData, nil, err
	}

	functionContextMap, err := functionContextToMap(functionContext)
	if err != nil {
		return parsedData, nil, err
	}

	env, err := NewCELEnv(CELVarOpts(), extraEnvOpts...)
	if err != nil {
		return parsedData, nil, fmt.Errorf("failed to create CEL environment: %v", err)
	}

	ast, issues := env.Compile(expression)
	if issues != nil {
		return parsedData, nil, fmt.Errorf("failed to compile expression: %v", issues)
	}

	program, err := env.Program(ast)
	if err != nil {
		return parsedData, nil, fmt.Errorf("failed to create program: %v", err)
	}

	output, err := yamlkit.VisitResourcesFiltered(parsedData, api.AttributeValueList{}, resourceProvider, options, func(doc *gaby.YamlDoc, output any, index int, resourceInfo *api.ResourceInfo) (any, []error) {
		accumulated := output.(api.AttributeValueList)

		var dataMap map[string]any
		if err := yaml.Unmarshal(doc.Bytes(), &dataMap); err != nil {
			return output, []error{err}
		}

		activation := CELActivation(dataMap, params, functionContextMap)
		val, _, err := program.Eval(activation)
		if err != nil {
			return output, []error{fmt.Errorf("CEL evaluation error on resource %s: %v", resourceInfo.ResourceName, err)}
		}

		attrValues, err := parseCELAttributeValueList(val)
		if err != nil {
			return output, []error{err}
		}
		return append(accumulated, attrValues...), nil
	})
	if err != nil {
		return parsedData, nil, err
	}

	return parsedData, output.(api.AttributeValueList), nil
}

// CelToGo deeply converts a CEL ref.Val to plain Go types, so that a result can be marshaled or
// type-switched without dealing in ref.Val. Exported for callers evaluating CEL outside the
// function executor, such as a server-side View column read from stored JSON.
func CelToGo(val ref.Val) any {
	if val == nil {
		return nil
	}
	goVal := val.Value()
	return celNativeToGo(goVal)
}

// celNativeToGo deeply converts CEL native values (which may contain ref.Val) to plain Go types.
func celNativeToGo(v any) any {
	switch val := v.(type) {
	case map[ref.Val]ref.Val:
		result := make(map[string]any, len(val))
		for k, v := range val {
			result[fmt.Sprintf("%v", k.Value())] = celNativeToGo(v.Value())
		}
		return result
	case map[string]any:
		result := make(map[string]any, len(val))
		for k, v := range val {
			result[k] = celNativeToGo(v)
		}
		return result
	case []ref.Val:
		result := make([]any, len(val))
		for i, v := range val {
			result[i] = celNativeToGo(v.Value())
		}
		return result
	case []any:
		result := make([]any, len(val))
		for i, v := range val {
			result[i] = celNativeToGo(v)
		}
		return result
	default:
		return v
	}
}

// parseCELAttributeValueList converts a CEL list of maps to an AttributeValueList.
func parseCELAttributeValueList(val ref.Val) (api.AttributeValueList, error) {
	goVal := CelToGo(val)
	list, ok := goVal.([]any)
	if !ok {
		return nil, fmt.Errorf("CEL expression must return a list, got %T", goVal)
	}

	values := make(api.AttributeValueList, 0, len(list))
	for i, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("each item in result must be a map, got %T at index %d", item, i)
		}
		values = append(values, celMapToAttributeValue(m))
	}
	return values, nil
}

// registerSetCEL registers the set-cel mutating function.
func registerSetCEL(fh handler.FunctionRegistry, converter configkit.ConfigConverter, resourceProvider yamlkit.ResourceProvider) {
	if err := fh.RegisterFunction("set-cel", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "set-cel",
			Parameters: []api.FunctionParameter{
				{
					ParameterName: "expression",
					Required:      true,
					Description:   "CEL expression that returns a partial resource as a map. Only the fields present in the result are modified; all other fields are preserved. The current resource is available as 'r' (alias 'object'). The 'params' dict and 'functionContext' map (ConfigHub metadata, e.g. functionContext.TargetID) are also available.",
					DataType:      api.DataTypeCEL,
					Example:       `{"spec": {"replicas": int(params.replicas)}}`,
				},
				{
					ParameterName: "param",
					Required:      false,
					Description:   "Parameters passed as key=value strings, accessible via the 'params' dict",
					DataType:      api.DataTypeString,
				},
			},
			VarArgs:  true,
			Mutating: true,
			Hermetic: true,
			// Idempotent and Replayable both depend on the expression, not on the function,
			// and the answer is the same one set-yq gives. Whether re-running produces the
			// same result is up to what the caller wrote; claiming false asserts something
			// this function cannot know. Replay needs the invocation to locate what it writes
			// by identity rather than by recorded position, and an expression evaluated per
			// resource under WhereResource does exactly that.
			Idempotent:            true,
			Replayable:            true,
			Description:           "Mutates configuration resources by merging a partial CEL expression result into the original resource. Comments are preserved.",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
			return GenericFnSetCEL(resourceProvider, fArgs.FunctionContext, fArgs.ParsedData, fArgs.Arguments, fArgs.Options)
		},
	}); err != nil {
		slog.Error("failed to register function", "error", err)
	}
}

// GenericFnSetCEL implements set-cel mutation. The CEL expression returns a
// partial resource map that is merged into the original resource using
// strategic merge (similar to Kubernetes ApplyConfiguration). Only the fields
// present in the CEL result are modified; all other fields are preserved.
// Extra CEL env options can be passed by toolchain-specific overrides.
func GenericFnSetCEL(resourceProvider yamlkit.ResourceProvider, functionContext *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, options *api.FunctionOptions, extraEnvOpts ...cel.EnvOption) (gaby.Container, any, error) {
	expression := args[0].Value.(string)

	params, err := CelParseParams(args, 1)
	if err != nil {
		return parsedData, nil, err
	}

	functionContextMap, err := functionContextToMap(functionContext)
	if err != nil {
		return parsedData, nil, err
	}

	env, err := NewCELEnv(CELVarOpts(), extraEnvOpts...)
	if err != nil {
		return parsedData, nil, fmt.Errorf("failed to create CEL environment: %v", err)
	}

	ast, issues := env.Compile(expression)
	if issues != nil {
		return parsedData, nil, fmt.Errorf("failed to compile expression: %v", issues)
	}

	program, err := env.Program(ast)
	if err != nil {
		return parsedData, nil, fmt.Errorf("failed to create program: %v", err)
	}

	originalData := []byte(parsedData.String())

	patched, changed, err := yamlkit.TransformConfig(originalData, resourceProvider, func(strippedParsed gaby.Container) ([]byte, error) {
		_, err := yamlkit.VisitResourcesFiltered(strippedParsed, nil, resourceProvider, options, func(doc *gaby.YamlDoc, output any, index int, resourceInfo *api.ResourceInfo) (any, []error) {
			var dataMap map[string]any
			if err := yaml.Unmarshal(doc.Bytes(), &dataMap); err != nil {
				return output, []error{fmt.Errorf("failed to unmarshal resource %d: %w", index, err)}
			}

			activation := CELActivation(dataMap, params, functionContextMap)
			val, _, err := program.Eval(activation)
			if err != nil {
				return output, []error{fmt.Errorf("CEL evaluation error on resource %d: %v", index, err)}
			}

			goVal := CelToGo(val)
			if goVal == nil {
				return output, []error{fmt.Errorf("CEL expression returned nil for resource %d", index)}
			}

			// Marshal the partial result and merge it into the original doc
			patchBytes, err := yaml.Marshal(goVal)
			if err != nil {
				return output, []error{fmt.Errorf("failed to marshal CEL result for resource %d: %w", index, err)}
			}
			patchDoc, err := gaby.ParseYAML(patchBytes)
			if err != nil {
				return output, []error{fmt.Errorf("failed to parse CEL result for resource %d: %w", index, err)}
			}
			if err := doc.MergeDoc(patchDoc); err != nil {
				return output, []error{fmt.Errorf("failed to merge CEL result into resource %d: %w", index, err)}
			}
			return output, nil
		})
		if err != nil {
			return nil, err
		}

		return []byte(strippedParsed.String()), nil
	}, options)
	if err != nil {
		return parsedData, nil, err
	}

	if !changed {
		return parsedData, nil, nil
	}

	newParsedData, err := gaby.ParseAll(patched)
	if err != nil {
		return parsedData, nil, fmt.Errorf("failed to parse patched data: %w", err)
	}
	return newParsedData, nil, nil
}

func genericFnCELValidate(resourceProvider yamlkit.ResourceProvider, options *api.FunctionOptions, functionContext *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument) (gaby.Container, any, error) {
	validationExpr := args[0].Value.(string)

	env, err := cel.NewEnv(
		cel.Variable("r", cel.DynType),
	)
	if err != nil {
		return parsedData, api.ValidationResultFalse, fmt.Errorf("failed to create CEL environment: %v", err)
	}

	expr, issues := env.Compile(validationExpr)
	if issues != nil {
		return parsedData, api.ValidationResultFalse, fmt.Errorf("failed to compile expression %s: %v", validationExpr, issues)
	}

	if !expr.OutputType().IsExactType(cel.BoolType) {
		return parsedData, api.ValidationResultFalse, fmt.Errorf("expression %s does not evaluate to a boolean", validationExpr)
	}

	program, err := env.Program(expr)
	if err != nil {
		return parsedData, api.ValidationResultFalse, fmt.Errorf("failed to create program for expression %s: %v", validationExpr, err)
	}

	type celValidateResult struct {
		passed  bool
		details []string
	}

	output, err := yamlkit.VisitResourcesFiltered(parsedData, &celValidateResult{passed: true}, resourceProvider, options, func(doc *gaby.YamlDoc, output any, index int, resourceInfo *api.ResourceInfo) (any, []error) {
		result := output.(*celValidateResult)
		var dataMap map[string]any
		if err := yaml.Unmarshal(doc.Bytes(), &dataMap); err != nil {
			return output, []error{fmt.Errorf("failed to unmarshal data for config %s: %v", functionContext.UnitSlug, err)}
		}

		obj := map[string]any{
			"r": dataMap,
		}

		val, _, err := program.Eval(obj)
		if err != nil {
			result.passed = false
			// Treat evaluation errors as expected and just fail the check, rather than parsing or expression errors.
			// There are many such error strings that the evaluator can return, so don't check them all.
			// Example prefixes:
			// "no such key:"
			// "index out of bounds:"
			// "no such attribute(s):"
			errorString := err.Error()
			result.details = append(result.details, "validation expression "+validationExpr+" could not be evaluated on resource "+string(resourceInfo.ResourceName)+": "+errorString)
			return result, nil
		}
		if val != types.True {
			result.passed = false
			result.details = append(result.details, "resource "+string(resourceInfo.ResourceName)+" failed validation expression "+validationExpr)
		}
		return result, nil
	})

	result := output.(*celValidateResult)
	if result.passed && err == nil {
		return parsedData, api.ValidationResultTrue, nil
	}

	failedResult := api.ValidationResultFalse
	failedResult.Details = result.details
	return parsedData, failedResult, err
}
