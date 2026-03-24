// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package generic

import (
	"fmt"

	"github.com/confighub/sdk/core/configkit"
	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/function/handler"
	"github.com/confighub/sdk/core/third_party/gaby"
)

func registerDeleteResource(fh handler.FunctionRegistry, converter configkit.ConfigConverter, resourceProvider yamlkit.ResourceProvider) {
	fh.RegisterFunction("delete-resource", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "delete-resource",
			Parameters: []api.FunctionParameter{
				{
					ParameterName: "resource-type",
					Required:      true,
					Description:   "Type (" + resourceProvider.TypeDescription() + ") of the resource to delete",
					DataType:      api.DataTypeString,
				},
				{
					ParameterName: "resource-name",
					Required:      true,
					Description:   "Name of the resource to delete",
					DataType:      api.DataTypeString,
				},
			},
			Mutating:              true,
			Validating:            false,
			Hermetic:              true,
			Idempotent:            false,
			Description:           "Remove the specified resource from the configuration data",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
			return genericFnDeleteResource(resourceProvider, fArgs.Options, fArgs.FunctionContext, fArgs.ParsedData, fArgs.Arguments)
		},
	})
}

func genericFnDeleteResource(resourceProvider yamlkit.ResourceProvider, options *api.FunctionOptions, functionContext *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument) (gaby.Container, any, error) {
	targetResourceType := api.ResourceType(args[0].Value.(string))
	targetResourceName := api.ResourceName(args[1].Value.(string))

	// Use VisitResources to find the existing resource and track its position
	whereExpressions := api.GetWhereResourceExpressions(options)
	foundIndex := -1
	visitor := func(doc *gaby.YamlDoc, output any, index int, resourceInfo *api.ResourceInfo) (any, []error) {
		if resourceInfo.ResourceType == targetResourceType &&
			resourceProvider.RemoveScopeFromResourceName(resourceInfo.ResourceName) == resourceProvider.RemoveScopeFromResourceName(targetResourceName) {
			foundIndex = index
		}
		return output, []error{}
	}

	_, err := yamlkit.VisitResourcesFiltered(parsedData, nil, resourceProvider, whereExpressions, visitor)
	if err != nil {
		return parsedData, nil, fmt.Errorf("failed to search for resource to delete: %v", err)
	}

	if foundIndex < 0 {
		return parsedData, nil, fmt.Errorf("resource with type %s and name %s not found", targetResourceType, targetResourceName)
	}

	// Remove the resource by creating a new slice without it
	newParsedData := make(gaby.Container, len(parsedData)-1)
	for i := 0; i < foundIndex; i++ {
		newParsedData[i] = parsedData[i]
	}
	for i := foundIndex + 1; i < len(parsedData); i++ {
		newParsedData[i-1] = parsedData[i]
	}

	return newParsedData, nil, nil
}
