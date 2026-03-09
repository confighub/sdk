// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package generic

import (
	"github.com/confighub/sdk/configkit"
	"github.com/confighub/sdk/configkit/yamlkit"
	"github.com/confighub/sdk/function/api"
	"github.com/confighub/sdk/function/handler"
	"github.com/confighub/sdk/third_party/gaby"
)

func registerSetReferencesOfType(fh handler.FunctionRegistry, converter configkit.ConfigConverter, resourceProvider yamlkit.ResourceProvider) {
	fh.RegisterFunction("set-references-of-type", &handler.FunctionRegistration{
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
			return genericFnSetReferencesOfType(resourceProvider, fArgs.FunctionContext, fArgs.ParsedData, fArgs.Arguments)
		},
	})
}

func attributeNameForResourceType(resourceType api.ResourceType) api.AttributeName {
	return api.AttributeName(string(api.AttributeNameResourceName) + "/" + string(resourceType))
}

func genericFnSetReferencesOfType(resourceProvider yamlkit.ResourceProvider, _ *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument) (gaby.Container, any, error) {
	resourceType := args[0].Value.(string)
	resourceName := args[1].Value.(string)

	var err error
	paths := yamlkit.GetPathRegistryForAttributeName(resourceProvider, attributeNameForResourceType(api.ResourceType(resourceType)))
	if paths != nil {
		err = yamlkit.UpdateStringPaths(parsedData, paths, []any{}, resourceProvider, resourceName, false, nil)
	}
	return parsedData, nil, err
}
