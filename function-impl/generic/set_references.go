// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package generic

import (
	"log/slog"

	"github.com/confighub/sdk/core/configkit"
	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/function/handler"
	"github.com/confighub/sdk/core/third_party/gaby"
)

func registerGetReferencesOfType(fh handler.FunctionRegistry, converter configkit.ConfigConverter, resourceProvider yamlkit.ResourceProvider) {
	if err := fh.RegisterFunction("get-references-of-type", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "get-references-of-type",
			Parameters: []api.FunctionParameter{
				{
					ParameterName: "resource-type",
					Required:      true,
					Description:   "Type (" + resourceProvider.TypeDescription() + ") of the config references to get",
					DataType:      api.DataTypeString,
				},
			},
			OutputInfo: &api.FunctionOutput{
				ResultName:  "references",
				Description: "Values of the references targeting the specified type",
				OutputType:  api.OutputTypeAttributeValueList,
				Schema:      &api.AttributeValueListSchema,
			},
			Mutating:              false,
			Validating:            false,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Gets references targeting the specified type",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
			return genericFnGetReferencesOfType(resourceProvider, fArgs.FunctionContext, fArgs.ParsedData, fArgs.Arguments, whereFromOptions(fArgs.Options))
		},
	}); err != nil {
		slog.Error("failed to register function", "error", err)
	}
}

func registerSetReferencesOfType(fh handler.FunctionRegistry, converter configkit.ConfigConverter, resourceProvider yamlkit.ResourceProvider) {
	if err := fh.RegisterFunction("set-references-of-type", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "set-references-of-type",
			Parameters: []api.FunctionParameter{
				{
					ParameterName: "resource-type",
					Required:      true,
					Description:   "Type (" + resourceProvider.TypeDescription() + ") of the config references to set",
					DataType:      api.DataTypeString,
				},
				{
					ParameterName: "resource-name",
					Required:      true,
					Description:   "Name to set in the resource references",
					DataType:      api.DataTypeString,
				},
			},
			Mutating:              true,
			Validating:            false,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Sets references targeting the specified type",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
			return genericFnSetReferencesOfType(resourceProvider, fArgs.FunctionContext, fArgs.ParsedData, fArgs.Arguments, whereFromOptions(fArgs.Options))
		},
	}); err != nil {
		slog.Error("failed to register function", "error", err)
	}
}

func attributeNameForResourceType(resourceType api.ResourceType) api.AttributeName {
	return api.AttributeName(string(api.AttributeNameResourceName) + "/" + string(resourceType))
}

func genericFnGetReferencesOfType(resourceProvider yamlkit.ResourceProvider, _ *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, whereExpressions []*api.VisitorRelationalExpression) (gaby.Container, any, error) {
	resourceType := args[0].Value.(string)

	paths := yamlkit.GetPathRegistryForAttributeName(resourceProvider, attributeNameForResourceType(api.ResourceType(resourceType)))
	if paths == nil {
		return parsedData, nil, nil
	}
	values, err := yamlkit.GetStringPaths(parsedData, paths, []any{}, resourceProvider, whereExpressions)
	return parsedData, values, err
}

func genericFnSetReferencesOfType(resourceProvider yamlkit.ResourceProvider, _ *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, whereExpressions []*api.VisitorRelationalExpression) (gaby.Container, any, error) {
	resourceType := args[0].Value.(string)
	resourceName := args[1].Value.(string)

	var err error
	paths := yamlkit.GetPathRegistryForAttributeName(resourceProvider, attributeNameForResourceType(api.ResourceType(resourceType)))
	if paths != nil {
		err = yamlkit.UpdateStringPaths(parsedData, paths, []any{}, resourceProvider, resourceName, false, whereExpressions)
	}
	return parsedData, nil, err
}
