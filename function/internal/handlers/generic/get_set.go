// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package generic

import (
	"github.com/cockroachdb/errors"
	"github.com/confighub/sdk/configkit"
	"github.com/confighub/sdk/configkit/yamlkit"
	"github.com/confighub/sdk/function/api"
	"github.com/confighub/sdk/function/handler"
	"github.com/confighub/sdk/third_party/gaby"
	"github.com/labstack/gommon/log"
	"github.com/swaggest/jsonschema-go"
)

// GetVisitorMapForPath is used to get visitor info for a resolved path.
func GetVisitorMapForPath(resourceProvider yamlkit.ResourceProvider, rt api.ResourceType, path api.UnresolvedPath) api.ResourceTypeToPathToVisitorInfoType {
	visitorInfo := yamlkit.GetPathVisitorInfo(resourceProvider, rt, path)
	if visitorInfo == nil {
		visitorInfo = &api.PathVisitorInfo{}
		visitorInfo.AttributeName = api.AttributeNameGeneral
		visitorInfo.Path = path
	} else {
		// Create a copy to modify
		specificVisitorInfo := *visitorInfo
		visitorInfo = &specificVisitorInfo
		// Path may be overridden below
	}
	if yamlkit.PathIsResolved(string(path), true) {
		visitorInfo.ResolvedPath = api.ResolvedPath(path)
	} else {
		visitorInfo.Path = path
	}
	resourceTypeToPaths := api.ResourceTypeToPathToVisitorInfoType{
		rt: {path: visitorInfo},
	}
	return resourceTypeToPaths
}

func registerGetStringPath(fh handler.FunctionRegistry, converter configkit.ConfigConverter, resourceProvider yamlkit.ResourceProvider) {
	fh.RegisterFunction("get-string-path", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "get-string-path",
			Parameters: []api.FunctionParameter{
				{
					ParameterName: "resource-type",
					Required:      true,
					Description:   "Resource type (" + resourceProvider.TypeDescription() + ") of the attribute to get",
					DataType:      api.DataTypeString,
				},
				{
					ParameterName:    "path",
					Required:         true,
					Description:      "Dot-separated configuration path of the attribute whose value to get. See https://docs.confighub.com/guide/functions/#configuration-path-syntax for more details regarding path syntax.",
					DataType:         api.DataTypeString,
					ValueConstraints: api.ValueConstraints{Regexp: api.PathRegexpString},
				},
			},
			OutputInfo: &api.FunctionOutput{
				ResultName:  "path",
				Description: "Value of the specified attribute path",
				OutputType:  api.OutputTypeAttributeValueList,
				Schema:      &attributeValueListSchema,
			},
			Mutating:              false,
			Validating:            false,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Returns the value(s) of the specified attribute path",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(functionContext *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument) (gaby.Container, any, error) {
			return GenericFnGetStringPath(resourceProvider, functionContext, parsedData, args)
		},
	})
}

func GenericFnGetStringPath(resourceProvider yamlkit.ResourceProvider, _ *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument) (gaby.Container, any, error) {
	// The argument value types should be verified before this function is called
	resourceType := args[0].Value.(string)
	unresolvedPath := args[1].Value.(string)

	resourceTypeToPaths := GetVisitorMapForPath(resourceProvider, api.ResourceType(resourceType), api.UnresolvedPath(unresolvedPath))
	values, err := yamlkit.GetStringPaths(parsedData, resourceTypeToPaths, []any{}, resourceProvider)
	return parsedData, values, err
}

func registerSetStringPath(fh handler.FunctionRegistry, converter configkit.ConfigConverter, resourceProvider yamlkit.ResourceProvider) {
	fh.RegisterFunction("set-string-path", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "set-string-path",
			Parameters: []api.FunctionParameter{
				{
					ParameterName: "resource-type",
					Required:      true,
					Description:   "Resource type (" + resourceProvider.TypeDescription() + ") of the attribute to set",
					DataType:      api.DataTypeString,
				},
				{
					ParameterName:    "path",
					Required:         true,
					Description:      "Dot-separated path of the attribute to set. See https://docs.confighub.com/guide/functions/#configuration-path-syntax for more details regarding path syntax.",
					DataType:         api.DataTypeString,
					ValueConstraints: api.ValueConstraints{Regexp: api.PathRegexpString},
				},
				{
					ParameterName: "attribute-value",
					Required:      true,
					Description:   "Value to set the attribute to",
					DataType:      api.DataTypeString,
				},
			},
			Mutating:              true,
			Validating:            false,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Set the value of the specified attribute path",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(functionContext *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument) (gaby.Container, any, error) {
			return GenericFnSetStringPath(resourceProvider, functionContext, parsedData, args, true)
		},
	})
}

func GenericFnSetStringPath(resourceProvider yamlkit.ResourceProvider, _ *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, upsert bool) (gaby.Container, any, error) {
	// The argument value types should be verified before this function is called
	resourceType := args[0].Value.(string)
	unresolvedPath := args[1].Value.(string)
	value := args[2].Value.(string)

	resourceTypeToPaths := GetVisitorMapForPath(resourceProvider, api.ResourceType(resourceType), api.UnresolvedPath(unresolvedPath))
	err := yamlkit.UpdateStringPaths(parsedData, resourceTypeToPaths, []any{}, resourceProvider, value, upsert)
	return parsedData, nil, err
}

func registerGetIntPath(fh handler.FunctionRegistry, converter configkit.ConfigConverter, resourceProvider yamlkit.ResourceProvider) {
	fh.RegisterFunction("get-int-path", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "get-int-path",
			Parameters: []api.FunctionParameter{
				{
					ParameterName: "resource-type",
					Required:      true,
					Description:   "Resource type (" + resourceProvider.TypeDescription() + ") of the attribute to get",
					DataType:      api.DataTypeString,
				},
				{
					ParameterName:    "path",
					Required:         true,
					Description:      "Dot-separated path of the attribute whose value to get. See https://docs.confighub.com/guide/functions/#configuration-path-syntax for more details regarding path syntax.",
					DataType:         api.DataTypeString,
					ValueConstraints: api.ValueConstraints{Regexp: api.PathRegexpString},
				},
			},
			OutputInfo: &api.FunctionOutput{
				ResultName:  "path",
				Description: "Value of the specified attribute path",
				OutputType:  api.OutputTypeAttributeValueList,
				Schema:      &attributeValueListSchema,
			},
			Mutating:              false,
			Validating:            false,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Returns the value(s) of the specified attribute path",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(functionContext *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument) (gaby.Container, any, error) {
			return GenericFnGetIntPath(resourceProvider, functionContext, parsedData, args)
		},
	})
}

func GenericFnGetIntPath(resourceProvider yamlkit.ResourceProvider, _ *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument) (gaby.Container, any, error) {
	// The argument value types should be verified before this function is called
	resourceType := args[0].Value.(string)
	unresolvedPath := args[1].Value.(string)

	resourceTypeToPaths := GetVisitorMapForPath(resourceProvider, api.ResourceType(resourceType), api.UnresolvedPath(unresolvedPath))
	values, err := yamlkit.GetPaths[int](parsedData, resourceTypeToPaths, []any{}, resourceProvider)
	return parsedData, values, err
}

func registerSetIntPath(fh handler.FunctionRegistry, converter configkit.ConfigConverter, resourceProvider yamlkit.ResourceProvider) {
	fh.RegisterFunction("set-int-path", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "set-int-path",
			Parameters: []api.FunctionParameter{
				{
					ParameterName: "resource-type",
					Required:      true,
					Description:   "Resource type (" + resourceProvider.TypeDescription() + ") of the attribute to set",
					DataType:      api.DataTypeString,
				},
				{
					ParameterName:    "path",
					Required:         true,
					Description:      "Dot-separated path of the attribute to set. See https://docs.confighub.com/guide/functions/#configuration-path-syntax for more details regarding path syntax.",
					DataType:         api.DataTypeString,
					ValueConstraints: api.ValueConstraints{Regexp: api.PathRegexpString},
				},
				{
					ParameterName: "attribute-value",
					Required:      true,
					Description:   "Value to set the attribute to",
					DataType:      api.DataTypeInt,
				},
			},
			Mutating:              true,
			Validating:            false,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Set the value of the specified attribute path",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(functionContext *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument) (gaby.Container, any, error) {
			return GenericFnSetIntPath(resourceProvider, functionContext, parsedData, args, true)
		},
	})
}

func GenericFnSetIntPath(resourceProvider yamlkit.ResourceProvider, _ *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, upsert bool) (gaby.Container, any, error) {
	// The argument value types should be verified before this function is called
	resourceType := args[0].Value.(string)
	unresolvedPath := args[1].Value.(string)
	value := args[2].Value.(int)

	resourceTypeToPaths := GetVisitorMapForPath(resourceProvider, api.ResourceType(resourceType), api.UnresolvedPath(unresolvedPath))
	err := yamlkit.UpdatePathsValue[int](parsedData, resourceTypeToPaths, []any{}, resourceProvider, value, upsert)
	return parsedData, nil, err
}

func registerGetBoolPath(fh handler.FunctionRegistry, converter configkit.ConfigConverter, resourceProvider yamlkit.ResourceProvider) {
	fh.RegisterFunction("get-bool-path", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "get-bool-path",
			Parameters: []api.FunctionParameter{
				{
					ParameterName: "resource-type",
					Required:      true,
					Description:   "Resource type (" + resourceProvider.TypeDescription() + ") of the attribute to get",
					DataType:      api.DataTypeString,
				},
				{
					ParameterName:    "path",
					Required:         true,
					Description:      "Dot-separated path of the attribute whose value to get. See https://docs.confighub.com/guide/functions/#configuration-path-syntax for more details regarding path syntax.",
					DataType:         api.DataTypeString,
					ValueConstraints: api.ValueConstraints{Regexp: api.PathRegexpString},
				},
			},
			OutputInfo: &api.FunctionOutput{
				ResultName:  "path",
				Description: "Value of the specified attribute path",
				OutputType:  api.OutputTypeAttributeValueList,
				Schema:      &attributeValueListSchema,
			},
			Mutating:              false,
			Validating:            false,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Returns the value(s) of the specified attribute path",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(functionContext *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument) (gaby.Container, any, error) {
			return GenericFnGetBoolPath(resourceProvider, functionContext, parsedData, args)
		},
	})
}

func GenericFnGetBoolPath(resourceProvider yamlkit.ResourceProvider, _ *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument) (gaby.Container, any, error) {
	// The argument value types should be verified before this function is called
	resourceType := args[0].Value.(string)
	unresolvedPath := args[1].Value.(string)

	resourceTypeToPaths := GetVisitorMapForPath(resourceProvider, api.ResourceType(resourceType), api.UnresolvedPath(unresolvedPath))
	values, err := yamlkit.GetPaths[bool](parsedData, resourceTypeToPaths, []any{}, resourceProvider)
	return parsedData, values, err
}

func registerSetBoolPath(fh handler.FunctionRegistry, converter configkit.ConfigConverter, resourceProvider yamlkit.ResourceProvider) {
	fh.RegisterFunction("set-bool-path", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "set-bool-path",
			Parameters: []api.FunctionParameter{
				{
					ParameterName: "resource-type",
					Required:      true,
					Description:   "Resource type (" + resourceProvider.TypeDescription() + ") of the attribute to set",
					DataType:      api.DataTypeString,
				},
				{
					ParameterName:    "path",
					Required:         true,
					Description:      "Dot-separated path of the attribute to set. See https://docs.confighub.com/guide/functions/#configuration-path-syntax for more details regarding path syntax.",
					DataType:         api.DataTypeString,
					ValueConstraints: api.ValueConstraints{Regexp: api.PathRegexpString},
				},
				{
					ParameterName: "attribute-value",
					Required:      true,
					Description:   "Value to set the attribute to",
					DataType:      api.DataTypeBool,
				},
			},
			Mutating:              true,
			Validating:            false,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Set the value of the specified attribute path",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(functionContext *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument) (gaby.Container, any, error) {
			return GenericFnSetBoolPath(resourceProvider, functionContext, parsedData, args, true)
		},
	})
}

func GenericFnSetBoolPath(resourceProvider yamlkit.ResourceProvider, _ *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, upsert bool) (gaby.Container, any, error) {
	// The argument value types should be verified before this function is called
	resourceType := args[0].Value.(string)
	unresolvedPath := args[1].Value.(string)
	value := args[2].Value.(bool)

	resourceTypeToPaths := GetVisitorMapForPath(resourceProvider, api.ResourceType(resourceType), api.UnresolvedPath(unresolvedPath))
	err := yamlkit.UpdatePathsValue[bool](parsedData, resourceTypeToPaths, []any{}, resourceProvider, value, upsert)
	return parsedData, nil, err
}

func registerSetPathComment(fh handler.FunctionRegistry, converter configkit.ConfigConverter, resourceProvider yamlkit.ResourceProvider) {
	fh.RegisterFunction("set-path-comment", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "set-path-comment",
			Parameters: []api.FunctionParameter{
				{
					ParameterName: "resource-type",
					Required:      true,
					Description:   "Resource type (" + resourceProvider.TypeDescription() + ") of the attribute to comment",
					DataType:      api.DataTypeString,
				},
				{
					ParameterName:    "path",
					Required:         true,
					Description:      "Dot-separated path of the attribute to comment. See https://docs.confighub.com/guide/functions/#configuration-path-syntax for more details regarding path syntax.",
					DataType:         api.DataTypeString,
					ValueConstraints: api.ValueConstraints{Regexp: api.PathRegexpString},
				},
				{
					ParameterName: "comment",
					Required:      true,
					Description:   "Comment to attach to the attribute",
					DataType:      api.DataTypeString,
				},
			},
			Mutating:              true,
			Validating:            false,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Set the comment of the specified attribute path",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(functionContext *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument) (gaby.Container, any, error) {
			return genericFnSetPathComment(resourceProvider, functionContext, parsedData, args)
		},
	})
}

func genericFnSetPathComment(resourceProvider yamlkit.ResourceProvider, _ *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument) (gaby.Container, any, error) {
	// The argument value types should be verified before this function is called
	resourceType := args[0].Value.(string)
	unresolvedPath := args[1].Value.(string)
	comment := args[2].Value.(string)

	resourceTypeToPaths := GetVisitorMapForPath(resourceProvider, api.ResourceType(resourceType), api.UnresolvedPath(unresolvedPath))
	visitor := func(doc *gaby.YamlDoc, output any, _ yamlkit.VisitorContext, currentDoc *gaby.YamlDoc) (any, error) {
		currentDoc.SetComment(comment)
		return output, nil
	}
	_, err := yamlkit.VisitPathsDoc(parsedData, resourceTypeToPaths, []any{}, nil, resourceProvider, visitor, false)
	return parsedData, nil, err
}

// Generalized path setter and getter functions moved from kubernetes/container_functions.go

func RegisterPathSetterAndGetter(
	fh handler.FunctionRegistry,
	name string,
	parameters []api.FunctionParameter,
	description string,
	attributeName api.AttributeName,
	resourceProvider yamlkit.ResourceProvider,
	addSetter bool,
	upsert bool,
) {
	resourceTypes := yamlkit.ResourceTypesForAttribute(attributeName, resourceProvider)
	numSetterParameters := len(parameters)
	// Note that there should be at least one parameter to describe the output.
	numGetterParameters := len(parameters) - 1
	valueParameter := numGetterParameters
	setterParameters := make([]api.FunctionParameter, numSetterParameters)
	for i := range setterParameters {
		setterParameters[i] = parameters[i]
		// All but the last parameter are path parameters
		if i < valueParameter {
			setterParameters[i].Description += "set"
		}
	}
	setterSignature := &api.FunctionSignature{
		FunctionName:          "set-" + name,
		Parameters:            setterParameters,
		Mutating:              true,
		Validating:            false,
		Hermetic:              true,
		Idempotent:            true,
		Description:           "Set" + description,
		FunctionType:          api.FunctionTypePathVisitor,
		AttributeName:         attributeName,
		AffectedResourceTypes: resourceTypes,
	}
	getterParameters := make([]api.FunctionParameter, numGetterParameters)
	for i := range getterParameters {
		getterParameters[i] = parameters[i]
		// All parameters are path parameters
		getterParameters[i].Description += "get"
	}
	reflector := jsonschema.Reflector{}
	attributeValueListSchema, err := reflector.Reflect(api.AttributeValueList{})
	if err != nil {
		log.Errorf("couldn't get schema for api.AttributeValueList")
	}
	// The getter output should match the last setter parameter
	outputInfo := &api.FunctionOutput{
		ResultName:  setterParameters[valueParameter].ParameterName,
		Description: setterParameters[valueParameter].Description,
		OutputType:  api.OutputTypeAttributeValueList,
		Schema:      &attributeValueListSchema,
	}
	getterSignature := &api.FunctionSignature{
		FunctionName:          "get-" + name,
		Parameters:            getterParameters,
		OutputInfo:            outputInfo,
		Mutating:              false,
		Validating:            false,
		Hermetic:              true,
		Idempotent:            true,
		Description:           "Get" + description,
		FunctionType:          api.FunctionTypePathVisitor,
		AttributeName:         attributeName,
		AffectedResourceTypes: resourceTypes,
	}
	var setterFunction, getterFunction handler.FunctionImplementation
	dataType := setterParameters[len(setterParameters)-1].DataType
	switch dataType {
	case api.DataTypeString:
		setterFunction = func(fc *api.FunctionContext, c gaby.Container, fa []api.FunctionArgument) (gaby.Container, any, error) {
			return genericFnSetStringVisitor(setterSignature, fc, c, fa, resourceProvider, upsert)
		}
		getterFunction = func(fc *api.FunctionContext, c gaby.Container, fa []api.FunctionArgument) (gaby.Container, any, error) {
			return genericFnGetStringVisitor(getterSignature, fc, c, fa, resourceProvider)
		}
	case api.DataTypeInt:
		setterFunction = func(fc *api.FunctionContext, c gaby.Container, fa []api.FunctionArgument) (gaby.Container, any, error) {
			return genericFnSetIntVisitor(setterSignature, fc, c, fa, resourceProvider, upsert)
		}
		getterFunction = func(fc *api.FunctionContext, c gaby.Container, fa []api.FunctionArgument) (gaby.Container, any, error) {
			return genericFnGetIntVisitor(getterSignature, fc, c, fa, resourceProvider)
		}
	case api.DataTypeBool:
		setterFunction = func(fc *api.FunctionContext, c gaby.Container, fa []api.FunctionArgument) (gaby.Container, any, error) {
			return genericFnSetBoolVisitor(setterSignature, fc, c, fa, resourceProvider, upsert)
		}
		getterFunction = func(fc *api.FunctionContext, c gaby.Container, fa []api.FunctionArgument) (gaby.Container, any, error) {
			return genericFnGetBoolVisitor(getterSignature, fc, c, fa, resourceProvider)
		}
	default:
		// Not supported
		log.Error("unsupported setter/getter data type " + string(dataType))
		return
	}
	if addSetter {
		fh.RegisterFunction("set-"+name, &handler.FunctionRegistration{
			FunctionSignature: *setterSignature,
			Function:          setterFunction,
		})
	}
	fh.RegisterFunction("get-"+name, &handler.FunctionRegistration{
		FunctionSignature: *getterSignature,
		Function:          getterFunction,
	})
}

func genericFnSetStringVisitor(signature *api.FunctionSignature, _ *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, resourceProvider yamlkit.ResourceProvider, upsert bool) (gaby.Container, any, error) {
	// The argument value types should be verified before this function is called

	// All but the last argument should be path arguments. The last argument is the value to set.
	pathArgs := make([]any, len(args)-1)
	for i := range pathArgs {
		pathArg, ok := args[i].Value.(string)
		if !ok {
			return parsedData, nil, errors.New("Invalid primary FunctionArgument")
		}
		safeArg := yamlkit.EscapeDotsInPathSegment(pathArg)
		pathArgs[i] = safeArg
	}
	valueToSet := args[len(args)-1].Value.(string)

	resourceTypeToPaths := yamlkit.GetPathRegistryForAttributeName(resourceProvider, signature.AttributeName)
	err := yamlkit.UpdateStringPaths(parsedData, resourceTypeToPaths, pathArgs, resourceProvider, valueToSet, upsert)
	return parsedData, nil, err
}

func genericFnGetStringVisitor(signature *api.FunctionSignature, _ *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, resourceProvider yamlkit.ResourceProvider) (gaby.Container, any, error) {
	// The argument value types should be verified before this function is called

	// All arguments should be path arguments.
	pathArgs := make([]any, len(args))
	for i := range pathArgs {
		pathArg, ok := args[i].Value.(string)
		if !ok {
			return parsedData, nil, errors.New("Invalid primary FunctionArgument")
		}
		safeArg := yamlkit.EscapeDotsInPathSegment(pathArg)
		pathArgs[i] = safeArg
	}

	resourceTypeToPaths := yamlkit.GetPathRegistryForAttributeName(resourceProvider, signature.AttributeName)
	values, err := yamlkit.GetStringPaths(parsedData, resourceTypeToPaths, pathArgs, resourceProvider)
	return parsedData, values, err
}

func genericFnSetIntVisitor(signature *api.FunctionSignature, _ *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, resourceProvider yamlkit.ResourceProvider, upsert bool) (gaby.Container, any, error) {
	// The argument value types should be verified before this function is called

	// All but the last argument should be path arguments. The last argument is the value to set.
	pathArgs := make([]any, len(args)-1)
	for i := range pathArgs {
		pathArg, ok := args[i].Value.(string)
		if !ok {
			return parsedData, nil, errors.New("Invalid primary FunctionArgument")
		}
		safeArg := yamlkit.EscapeDotsInPathSegment(pathArg)
		pathArgs[i] = safeArg
	}
	valueToSet := args[len(args)-1].Value.(int)

	resourceTypeToPaths := yamlkit.GetPathRegistryForAttributeName(resourceProvider, signature.AttributeName)
	err := yamlkit.UpdatePathsValue[int](parsedData, resourceTypeToPaths, pathArgs, resourceProvider, valueToSet, upsert)
	return parsedData, nil, err
}

func genericFnGetIntVisitor(signature *api.FunctionSignature, _ *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, resourceProvider yamlkit.ResourceProvider) (gaby.Container, any, error) {
	// The argument value types should be verified before this function is called

	// All arguments should be path arguments.
	pathArgs := make([]any, len(args))
	for i := range pathArgs {
		pathArg, ok := args[i].Value.(string)
		if !ok {
			return parsedData, nil, errors.New("Invalid primary FunctionArgument")
		}
		safeArg := yamlkit.EscapeDotsInPathSegment(pathArg)
		pathArgs[i] = safeArg
	}

	resourceTypeToPaths := yamlkit.GetPathRegistryForAttributeName(resourceProvider, signature.AttributeName)
	values, err := yamlkit.GetPaths[int](parsedData, resourceTypeToPaths, pathArgs, resourceProvider)
	return parsedData, values, err
}

func genericFnSetBoolVisitor(signature *api.FunctionSignature, _ *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, resourceProvider yamlkit.ResourceProvider, upsert bool) (gaby.Container, any, error) {
	// The argument value types should be verified before this function is called

	// All but the last argument should be path arguments. The last argument is the value to set.
	pathArgs := make([]any, len(args)-1)
	for i := range pathArgs {
		pathArg, ok := args[i].Value.(string)
		if !ok {
			return parsedData, nil, errors.New("Invalid primary FunctionArgument")
		}
		safeArg := yamlkit.EscapeDotsInPathSegment(pathArg)
		pathArgs[i] = safeArg
	}
	valueToSet := args[len(args)-1].Value.(bool)

	resourceTypeToPaths := yamlkit.GetPathRegistryForAttributeName(resourceProvider, signature.AttributeName)
	err := yamlkit.UpdatePathsValue[bool](parsedData, resourceTypeToPaths, pathArgs, resourceProvider, valueToSet, upsert)
	return parsedData, nil, err
}

func genericFnGetBoolVisitor(signature *api.FunctionSignature, _ *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, resourceProvider yamlkit.ResourceProvider) (gaby.Container, any, error) {
	// The argument value types should be verified before this function is called

	// All arguments should be path arguments.
	pathArgs := make([]any, len(args))
	for i := range pathArgs {
		pathArg, ok := args[i].Value.(string)
		if !ok {
			return parsedData, nil, errors.New("Invalid primary FunctionArgument")
		}
		safeArg := yamlkit.EscapeDotsInPathSegment(pathArg)
		pathArgs[i] = safeArg
	}

	resourceTypeToPaths := yamlkit.GetPathRegistryForAttributeName(resourceProvider, signature.AttributeName)
	values, err := yamlkit.GetPaths[bool](parsedData, resourceTypeToPaths, pathArgs, resourceProvider)
	return parsedData, values, err
}
