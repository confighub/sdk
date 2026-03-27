// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package generic

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/confighub/sdk/core/configkit"
	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/function/handler"
	"github.com/confighub/sdk/core/third_party/gaby"
	starlarkjson "go.starlark.net/lib/json"
	"go.starlark.net/starlark"
	"go.starlark.net/syntax"
	"sigs.k8s.io/yaml"
)

// parseParams extracts key=value params from function arguments starting at the given index.
func parseParams(args []api.FunctionArgument, startIndex int) (*starlark.Dict, error) {
	params := starlark.NewDict(len(args) - startIndex)
	for i := startIndex; i < len(args); i++ {
		s, ok := args[i].Value.(string)
		if !ok {
			return nil, fmt.Errorf("param argument %d must be a string, got %T", i, args[i].Value)
		}
		key, value, found := strings.Cut(s, "=")
		if !found {
			return nil, fmt.Errorf("param argument %d must be in key=value format, got %q", i, s)
		}
		if err := params.SetKey(starlark.String(key), starlark.String(value)); err != nil {
			return nil, err
		}
	}
	return params, nil
}

// starlarkPredeclared builds the common predeclared variables for Starlark execution.
// The rDict is set as both "r" and "object" (same pointer, so mutations through either name
// affect the same dict).
func starlarkPredeclared(rDict *starlark.Dict, params *starlark.Dict) starlark.StringDict {
	predeclared := starlark.StringDict{
		"r":      rDict,
		"object": rDict,
		"json":   starlarkjson.Module,
		"re":     starlarkReModule,
	}
	if params != nil {
		predeclared["params"] = params
	} else {
		predeclared["params"] = starlark.NewDict(0)
	}
	return predeclared
}

var starlarkFileOpts = syntax.FileOptions{
	Set:             true,
	GlobalReassign:  true,
	TopLevelControl: true,
	Recursion:       true,
}

// runStarlarkForResource executes a Starlark program with a single resource as input.
// The resource is available as 'r' and 'object' (both point to the same dict).
// Returns the globals and the effective value of r after execution (which may have been reassigned).
func runStarlarkForResource(programName, program string, resource map[string]any, params *starlark.Dict) (starlark.StringDict, starlark.Value, error) {
	thread := &starlark.Thread{Name: programName}

	rDict, err := goToStarlark(resource)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to convert resource to starlark value: %w", err)
	}

	predeclared := starlarkPredeclared(rDict.(*starlark.Dict), params)

	globals, err := starlark.ExecFileOptions(&starlarkFileOpts, thread, programName, program, predeclared)
	if err != nil {
		if evalErr, ok := err.(*starlark.EvalError); ok {
			return nil, nil, fmt.Errorf("starlark error: %s", evalErr.Backtrace())
		}
		return nil, nil, fmt.Errorf("starlark error: %w", err)
	}

	// Check if r was reassigned (GlobalReassign is enabled)
	if newR, ok := globals["r"]; ok {
		return globals, newR, nil
	}
	return globals, rDict, nil
}

// compileStarlarkProgram executes a Starlark program once with an empty resource dict
// to obtain function definitions (validate, extract, etc.). The returned globals contain
// the defined functions which can then be called per-resource.
func compileStarlarkProgram(programName, program string, params *starlark.Dict) (starlark.StringDict, error) {
	thread := &starlark.Thread{Name: programName}

	emptyDict := starlark.NewDict(0)
	predeclared := starlarkPredeclared(emptyDict, params)

	globals, err := starlark.ExecFileOptions(&starlarkFileOpts, thread, programName, program, predeclared)
	if err != nil {
		if evalErr, ok := err.(*starlark.EvalError); ok {
			return nil, fmt.Errorf("starlark error: %s", evalErr.Backtrace())
		}
		return nil, fmt.Errorf("starlark error: %w", err)
	}

	return globals, nil
}

// docToStarlarkDict converts a gaby.YamlDoc to a Starlark dict.
func docToStarlarkDict(doc *gaby.YamlDoc) (*starlark.Dict, error) {
	var dataMap map[string]any
	if err := yaml.Unmarshal(doc.Bytes(), &dataMap); err != nil {
		return nil, fmt.Errorf("failed to unmarshal resource: %w", err)
	}
	val, err := goToStarlark(dataMap)
	if err != nil {
		return nil, err
	}
	return val.(*starlark.Dict), nil
}

// goToStarlark converts a Go value to a Starlark value.
func goToStarlark(v any) (starlark.Value, error) {
	switch val := v.(type) {
	case nil:
		return starlark.None, nil
	case bool:
		return starlark.Bool(val), nil
	case int:
		return starlark.MakeInt(val), nil
	case int64:
		return starlark.MakeInt64(val), nil
	case float64:
		return starlark.Float(val), nil
	case string:
		return starlark.String(val), nil
	case []any:
		list := make([]starlark.Value, 0, len(val))
		for _, item := range val {
			sv, err := goToStarlark(item)
			if err != nil {
				return nil, err
			}
			list = append(list, sv)
		}
		return starlark.NewList(list), nil
	case []map[string]any:
		list := make([]starlark.Value, 0, len(val))
		for _, item := range val {
			sv, err := goToStarlark(item)
			if err != nil {
				return nil, err
			}
			list = append(list, sv)
		}
		return starlark.NewList(list), nil
	case map[string]any:
		dict := starlark.NewDict(len(val))
		for k, v := range val {
			sv, err := goToStarlark(v)
			if err != nil {
				return nil, err
			}
			if err := dict.SetKey(starlark.String(k), sv); err != nil {
				return nil, err
			}
		}
		return dict, nil
	default:
		// Fall back to JSON round-trip for complex types
		jsonBytes, err := json.Marshal(v)
		if err != nil {
			return nil, fmt.Errorf("cannot convert %T to starlark value: %w", v, err)
		}
		return starlark.String(string(jsonBytes)), nil
	}
}

// starlarkToGo converts a Starlark value to a Go value.
func starlarkToGo(v starlark.Value) (any, error) {
	switch val := v.(type) {
	case starlark.NoneType:
		return nil, nil
	case starlark.Bool:
		return bool(val), nil
	case starlark.Int:
		i, ok := val.Int64()
		if ok {
			return i, nil
		}
		return val.String(), nil
	case starlark.Float:
		return float64(val), nil
	case starlark.String:
		return string(val), nil
	case *starlark.List:
		result := make([]any, 0, val.Len())
		for i := 0; i < val.Len(); i++ {
			goVal, err := starlarkToGo(val.Index(i))
			if err != nil {
				return nil, err
			}
			result = append(result, goVal)
		}
		return result, nil
	case starlark.Tuple:
		result := make([]any, 0, len(val))
		for _, item := range val {
			goVal, err := starlarkToGo(item)
			if err != nil {
				return nil, err
			}
			result = append(result, goVal)
		}
		return result, nil
	case *starlark.Dict:
		result := make(map[string]any, val.Len())
		for _, item := range val.Items() {
			key, err := starlarkToGo(item[0])
			if err != nil {
				return nil, err
			}
			keyStr, ok := key.(string)
			if !ok {
				keyStr = fmt.Sprintf("%v", key)
			}
			goVal, err := starlarkToGo(item[1])
			if err != nil {
				return nil, err
			}
			result[keyStr] = goVal
		}
		return result, nil
	default:
		return val.String(), nil
	}
}

// registerSetStarlark registers the set-starlark mutating function.
func registerSetStarlark(fh handler.FunctionRegistry, converter configkit.ConfigConverter, resourceProvider yamlkit.ResourceProvider) {
	fh.RegisterFunction("set-starlark", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "set-starlark",
			Parameters: []api.FunctionParameter{
				{
					ParameterName: "program",
					Required:      true,
					Description:   "Starlark program that mutates a resource. The variable 'r' (alias 'object') is a dict representing the current resource. Modify r in place. The program is executed once per resource. The 'json', 're' modules and 'params' dict are also available.",
					DataType:      api.DataTypeString,
				},
				{
					ParameterName: "param",
					Required:      false,
					Description:   "Parameters passed to the Starlark program as key=value strings, accessible via the 'params' dict",
					DataType:      api.DataTypeString,
				},
			},
			VarArgs:               true,
			Mutating:              true,
			Hermetic:              true,
			Idempotent:            false,
			Description:           "Mutates configuration resources using a Starlark program executed per resource. Comments are preserved. The 're' module provides regex support (search, match, sub, findall).",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
			return genericFnSetStarlark(resourceProvider, fArgs.Options, fArgs.ParsedData, fArgs.Arguments)
		},
	})
}

func genericFnSetStarlark(resourceProvider yamlkit.ResourceProvider, options *api.FunctionOptions, parsedData gaby.Container, args []api.FunctionArgument) (gaby.Container, any, error) {
	program := args[0].Value.(string)

	params, err := parseParams(args, 1)
	if err != nil {
		return parsedData, nil, err
	}

	// Get original serialized data for comment preservation
	originalData := []byte(parsedData.String())

	whereExpressions := api.GetWhereResourceExpressions(options)

	// Use TransformConfig for comment-preserving mutation
	patched, changed, err := yamlkit.TransformConfig(originalData, resourceProvider, func(strippedParsed gaby.Container) ([]byte, error) {
		// Execute the Starlark program once per matching resource
		modifiedResources := make([]any, len(strippedParsed))
		_, err := yamlkit.VisitResourcesFiltered(strippedParsed, nil, resourceProvider, whereExpressions, func(doc *gaby.YamlDoc, output any, index int, resourceInfo *api.ResourceInfo) (any, []error) {
			var dataMap map[string]any
			if err := yaml.Unmarshal(doc.Bytes(), &dataMap); err != nil {
				return output, []error{fmt.Errorf("failed to unmarshal resource %d: %w", index, err)}
			}

			_, rVal, err := runStarlarkForResource("set-starlark", program, dataMap, params)
			if err != nil {
				return output, []error{err}
			}

			goVal, err := starlarkToGo(rVal)
			if err != nil {
				return output, []error{fmt.Errorf("failed to convert starlark result for resource %d: %w", index, err)}
			}
			modifiedResources[index] = goVal
			return output, nil
		})
		if err != nil {
			return nil, err
		}
		// Fill in unmodified resources (those that didn't match the filter)
		for i, doc := range strippedParsed {
			if modifiedResources[i] == nil {
				var dataMap map[string]any
				if err := yaml.Unmarshal(doc.Bytes(), &dataMap); err != nil {
					return nil, fmt.Errorf("failed to unmarshal resource %d: %w", i, err)
				}
				modifiedResources[i] = dataMap
			}
		}
		return marshalResourceList(modifiedResources)
	})
	if err != nil {
		return parsedData, nil, err
	}

	if !changed {
		return parsedData, nil, nil
	}

	// Re-parse the patched data
	newParsedData, err := gaby.ParseAll(patched)
	if err != nil {
		return parsedData, nil, fmt.Errorf("failed to parse patched data: %w", err)
	}
	return newParsedData, nil, nil
}

// registerVetStarlark registers the vet-starlark validation function.
func registerVetStarlark(fh handler.FunctionRegistry, converter configkit.ConfigConverter, resourceProvider yamlkit.ResourceProvider) {
	fh.RegisterFunction("vet-starlark", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "vet-starlark",
			Parameters: []api.FunctionParameter{
				{
					ParameterName: "program",
					Required:      true,
					Description:   "Starlark program that validates resources. Must define a 'validate(r)' function that takes a resource dict and returns a dict with 'passed' (bool) and optionally 'details' (list of strings). The function is called once per resource. The 'json', 're' modules and 'params' dict are available.",
					DataType:      api.DataTypeString,
					Example: `def validate(r):
  if r.get("kind") == "Deployment":
    containers = r["spec"]["template"]["spec"]["containers"]
    for c in containers:
      if "resources" not in c:
        return {"passed": False, "details": ["container " + c["name"] + " missing resources"]}
  return {"passed": True}`,
				},
				{
					ParameterName: "param",
					Required:      false,
					Description:   "Parameters passed to the Starlark program as key=value strings, accessible via the 'params' dict",
					DataType:      api.DataTypeString,
				},
			},
			VarArgs: true,
			OutputInfo: &api.FunctionOutput{
				ResultName:  "validation",
				Description: "Validation result from Starlark program",
				OutputType:  api.OutputTypeValidationResult,
				Schema:      &api.ValidationResultListSchema,
			},
			Mutating:              false,
			Validating:            true,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Validates configuration resources using a Starlark program that defines a validate(r) function returning {passed: bool, details: [string]}. Called once per resource.",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
			return genericFnVetStarlark(resourceProvider, fArgs.Options, fArgs.ParsedData, fArgs.Arguments)
		},
	})
}

func genericFnVetStarlark(resourceProvider yamlkit.ResourceProvider, options *api.FunctionOptions, parsedData gaby.Container, args []api.FunctionArgument) (gaby.Container, any, error) {
	program := args[0].Value.(string)

	params, err := parseParams(args, 1)
	if err != nil {
		return parsedData, api.ValidationResultFalse, err
	}

	// Compile the program once to get the validate function definition
	globals, err := compileStarlarkProgram("vet-starlark", program, params)
	if err != nil {
		failedResult := api.ValidationResultFalse
		failedResult.Details = []string{err.Error()}
		return parsedData, failedResult, nil
	}

	validateFn, ok := globals["validate"]
	if !ok {
		return parsedData, api.ValidationResultFalse, errors.New("starlark program must define a 'validate' function")
	}
	callable, ok := validateFn.(starlark.Callable)
	if !ok {
		return parsedData, api.ValidationResultFalse, errors.New("'validate' must be a callable function")
	}

	// Call validate(r) for each resource, accumulating results
	overallResult := api.ValidationResult{Passed: true}

	whereExpressions := api.GetWhereResourceExpressions(options)
	_, err = yamlkit.VisitResourcesFiltered(parsedData, nil, resourceProvider, whereExpressions, func(doc *gaby.YamlDoc, output any, index int, resourceInfo *api.ResourceInfo) (any, []error) {
		rDict, err := docToStarlarkDict(doc)
		if err != nil {
			return output, []error{err}
		}

		thread := &starlark.Thread{Name: "vet-starlark-validate"}
		result, err := starlark.Call(thread, callable, starlark.Tuple{rDict}, nil)
		if err != nil {
			if evalErr, ok := err.(*starlark.EvalError); ok {
				overallResult.Passed = false
				overallResult.Details = append(overallResult.Details, evalErr.Backtrace())
			} else {
				overallResult.Passed = false
				overallResult.Details = append(overallResult.Details, err.Error())
			}
			return output, nil
		}

		vr, err := parseValidationResult(result)
		if err != nil {
			return output, []error{err}
		}
		if !vr.Passed {
			overallResult.Passed = false
		}
		overallResult.Details = append(overallResult.Details, vr.Details...)
		overallResult.FailedAttributes = append(overallResult.FailedAttributes, vr.FailedAttributes...)
		return output, nil
	})
	if err != nil {
		return parsedData, api.ValidationResultFalse, err
	}

	return parsedData, overallResult, nil
}

// parseValidationResult converts a Starlark value to a ValidationResult.
func parseValidationResult(result starlark.Value) (api.ValidationResult, error) {
	// If it's a bool, simple pass/fail
	if boolVal, ok := result.(starlark.Bool); ok {
		return api.ValidationResult{Passed: bool(boolVal)}, nil
	}

	// If it's a dict, extract passed and details
	dict, ok := result.(*starlark.Dict)
	if !ok {
		return api.ValidationResultFalse, fmt.Errorf("validate must return a bool or dict, got %s", result.Type())
	}

	passedVal, found, err := dict.Get(starlark.String("passed"))
	if err != nil {
		return api.ValidationResultFalse, err
	}
	if !found {
		return api.ValidationResultFalse, errors.New("validate result dict must contain 'passed' key")
	}
	passedBool, ok := passedVal.(starlark.Bool)
	if !ok {
		return api.ValidationResultFalse, errors.New("'passed' must be a bool")
	}

	vr := api.ValidationResult{Passed: bool(passedBool)}

	detailsVal, found, err := dict.Get(starlark.String("details"))
	if err != nil {
		return vr, nil
	}
	if found {
		detailsList, ok := detailsVal.(*starlark.List)
		if ok {
			for i := 0; i < detailsList.Len(); i++ {
				vr.Details = append(vr.Details, detailsList.Index(i).String())
			}
			// Strip quotes from starlark string representations
			for i, d := range vr.Details {
				if len(d) >= 2 && d[0] == '"' && d[len(d)-1] == '"' {
					vr.Details[i] = d[1 : len(d)-1]
				}
			}
		}
	}

	failedVal, found, err := dict.Get(starlark.String("failed_attributes"))
	if err != nil {
		return vr, nil
	}
	if found {
		failedAttrs, err := parseAttributeValueList(failedVal)
		if err != nil {
			return vr, fmt.Errorf("failed to parse failed_attributes: %w", err)
		}
		vr.FailedAttributes = failedAttrs
	}

	return vr, nil
}

// registerGetStarlark registers the get-starlark extraction function.
func registerGetStarlark(fh handler.FunctionRegistry, converter configkit.ConfigConverter, resourceProvider yamlkit.ResourceProvider) {
	fh.RegisterFunction("get-starlark", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "get-starlark",
			Parameters: []api.FunctionParameter{
				{
					ParameterName: "program",
					Required:      true,
					Description:   "Starlark program that extracts values from resources. Must define an 'extract(r)' function that takes a resource dict and returns a list of dicts, each with fields: ResourceName (string), ResourceType (string), Path (string), Value (any), and optionally AttributeName (string), DataType (string). The function is called once per resource. The 'json', 're' modules and 'params' dict are available.",
					DataType:      api.DataTypeString,
					Example: `def extract(r):
  values = []
  if r.get("kind") == "Deployment":
    for c in r["spec"]["template"]["spec"]["containers"]:
      values.append({
        "ResourceName": r["metadata"].get("namespace", "") + "/" + r["metadata"]["name"],
        "ResourceType": r["apiVersion"] + "/" + r["kind"],
        "Path": "spec.template.spec.containers." + c["name"] + ".image",
        "Value": c["image"],
      })
  return values`,
				},
				{
					ParameterName: "param",
					Required:      false,
					Description:   "Parameters passed to the Starlark program as key=value strings, accessible via the 'params' dict",
					DataType:      api.DataTypeString,
				},
			},
			VarArgs: true,
			OutputInfo: &api.FunctionOutput{
				ResultName:  "attributes",
				Description: "Extracted attribute values from Starlark program",
				OutputType:  api.OutputTypeAttributeValueList,
				Schema:      &api.AttributeValueListSchema,
			},
			Mutating:              false,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Extracts attribute values from configuration resources using a Starlark program that defines an extract(r) function returning an AttributeValueList. Called once per resource.",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
			return genericFnGetStarlark(resourceProvider, fArgs.Options, fArgs.ParsedData, fArgs.Arguments)
		},
	})
}

func genericFnGetStarlark(resourceProvider yamlkit.ResourceProvider, options *api.FunctionOptions, parsedData gaby.Container, args []api.FunctionArgument) (gaby.Container, any, error) {
	program := args[0].Value.(string)

	params, err := parseParams(args, 1)
	if err != nil {
		return parsedData, nil, err
	}

	// Compile the program once to get the extract function definition
	globals, err := compileStarlarkProgram("get-starlark", program, params)
	if err != nil {
		return parsedData, nil, err
	}

	extractFn, ok := globals["extract"]
	if !ok {
		return parsedData, nil, errors.New("starlark program must define an 'extract' function")
	}
	callable, ok := extractFn.(starlark.Callable)
	if !ok {
		return parsedData, nil, errors.New("'extract' must be a callable function")
	}

	// Call extract(r) for each resource, accumulating results
	whereExpressions := api.GetWhereResourceExpressions(options)
	output, err := yamlkit.VisitResourcesFiltered(parsedData, api.AttributeValueList{}, resourceProvider, whereExpressions, func(doc *gaby.YamlDoc, output any, index int, resourceInfo *api.ResourceInfo) (any, []error) {
		accumulated := output.(api.AttributeValueList)

		rDict, err := docToStarlarkDict(doc)
		if err != nil {
			return output, []error{err}
		}

		thread := &starlark.Thread{Name: "get-starlark-extract"}
		result, err := starlark.Call(thread, callable, starlark.Tuple{rDict}, nil)
		if err != nil {
			if evalErr, ok := err.(*starlark.EvalError); ok {
				return output, []error{fmt.Errorf("starlark error: %s", evalErr.Backtrace())}
			}
			return output, []error{fmt.Errorf("starlark error: %w", err)}
		}

		attrValues, err := parseAttributeValueList(result)
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

// parseAttributeValueList converts a Starlark list of dicts to AttributeValueList.
func parseAttributeValueList(result starlark.Value) (api.AttributeValueList, error) {
	list, ok := result.(*starlark.List)
	if !ok {
		return nil, fmt.Errorf("extract must return a list, got %s", result.Type())
	}

	values := make(api.AttributeValueList, 0, list.Len())
	for i := 0; i < list.Len(); i++ {
		item := list.Index(i)
		dict, ok := item.(*starlark.Dict)
		if !ok {
			return nil, fmt.Errorf("each item in extract result must be a dict, got %s at index %d", item.Type(), i)
		}

		av := api.AttributeValue{}

		if v, found, _ := dict.Get(starlark.String("ResourceName")); found {
			av.ResourceName = api.ResourceName(starlarkStringValue(v))
		}
		if v, found, _ := dict.Get(starlark.String("ResourceType")); found {
			av.ResourceType = api.ResourceType(starlarkStringValue(v))
		}
		if v, found, _ := dict.Get(starlark.String("Path")); found {
			av.Path = api.ResolvedPath(starlarkStringValue(v))
		}
		if v, found, _ := dict.Get(starlark.String("Value")); found {
			goVal, err := starlarkToGo(v)
			if err != nil {
				return nil, fmt.Errorf("failed to convert Value at index %d: %w", i, err)
			}
			av.Value = goVal
		}
		if v, found, _ := dict.Get(starlark.String("AttributeName")); found {
			av.AttributeName = api.AttributeName(starlarkStringValue(v))
		}
		if v, found, _ := dict.Get(starlark.String("DataType")); found {
			av.DataType = api.DataType(starlarkStringValue(v))
		}
		if v, found, _ := dict.Get(starlark.String("Comment")); found {
			av.Comment = starlarkStringValue(v)
		}

		values = append(values, av)
	}

	return values, nil
}

// starlarkStringValue extracts a string from a Starlark value.
func starlarkStringValue(v starlark.Value) string {
	if s, ok := v.(starlark.String); ok {
		return string(s)
	}
	return v.String()
}

// marshalResourceList converts a list of resources to YAML bytes.
func marshalResourceList(resources []any) ([]byte, error) {
	var result []byte
	for i, r := range resources {
		yamlBytes, err := yaml.Marshal(r)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal resource %d: %w", i, err)
		}
		if i > 0 {
			result = append(result, []byte("---\n")...)
		}
		result = append(result, yamlBytes...)
	}
	return result, nil
}
