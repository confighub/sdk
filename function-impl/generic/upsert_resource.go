// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package generic

import (
	"encoding/json"
	"fmt"

	"github.com/confighub/sdk/core/configkit"
	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/function/handler"
	"github.com/confighub/sdk/core/third_party/gaby"
)

func registerUpsertResource(fh handler.FunctionRegistry, converter configkit.ConfigConverter, resourceProvider yamlkit.ResourceProvider) {
	fh.RegisterFunction("upsert-resource", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "upsert-resource",
			Parameters: []api.FunctionParameter{
				{
					ParameterName:    "resource-list",
					Required:         true,
					Description:      "ResourceList containing the resource to upsert",
					DataType:         api.DataTypeResourceList,
					ValueConstraints: api.ValueConstraints{Schema: &api.ResourceListSchema},
				},
				{
					ParameterName: "resource-type",
					Required:      true,
					Description:   "Type (" + resourceProvider.TypeDescription() + ") of the resource to upsert",
					DataType:      api.DataTypeString,
				},
				{
					ParameterName: "resource-name",
					Required:      true,
					Description:   "Name of the resource to upsert",
					DataType:      api.DataTypeString,
				},
			},
			Mutating:              true,
			Validating:            false,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Append the resource if it is not present or replace the existing resource if it is already present in the configuration data. Intended to be used with get-resources.",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
			return genericFnUpsertResource(resourceProvider, fArgs.FunctionContext, fArgs.ParsedData, fArgs.Arguments)
		},
	})
}

func genericFnUpsertResource(resourceProvider yamlkit.ResourceProvider, functionContext *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument) (gaby.Container, any, error) {
	// Unmarshal the first argument into api.ResourceList
	resourceListString := args[0].Value.(string)
	var resourceList api.ResourceList
	err := json.Unmarshal([]byte(resourceListString), &resourceList)
	if err != nil {
		return parsedData, nil, fmt.Errorf("failed to unmarshal resource-list argument: %v", err)
	}

	if len(resourceList) == 0 {
		return parsedData, nil, fmt.Errorf("resource-list cannot be empty")
	}

	targetResourceType := api.ResourceType(args[1].Value.(string))
	targetResourceName := api.ResourceName(args[2].Value.(string))

	// Find the resource to upsert from the resource list
	var resourceToUpsert *api.Resource
	for i := range resourceList {
		if resourceList[i].ResourceType == targetResourceType &&
			resourceProvider.RemoveScopeFromResourceName(resourceList[i].ResourceName) == resourceProvider.RemoveScopeFromResourceName(targetResourceName) {
			resourceToUpsert = &resourceList[i]
			break
		}
	}

	if resourceToUpsert == nil {
		return parsedData, nil, fmt.Errorf("resource with type %s and name %s not found in resource-list", targetResourceType, targetResourceName)
	}

	// Parse the resource body to get a document we can insert/replace
	resourceDoc, err := gaby.ParseYAML([]byte(resourceToUpsert.ResourceBody))
	if err != nil {
		return parsedData, nil, fmt.Errorf("failed to parse resource body: %v", err)
	}

	// Use VisitResources to find the existing resource and track its position
	foundIndex := -1
	visitor := func(doc *gaby.YamlDoc, output any, index int, resourceInfo *api.ResourceInfo) (any, []error) {
		if resourceInfo.ResourceType == targetResourceType &&
			resourceProvider.RemoveScopeFromResourceName(resourceInfo.ResourceName) == resourceProvider.RemoveScopeFromResourceName(targetResourceName) {
			foundIndex = index
		}
		return output, []error{}
	}

	_, err = yamlkit.VisitResources(parsedData, nil, resourceProvider, visitor)
	if err != nil {
		return parsedData, nil, fmt.Errorf("failed to search for existing resource: %v", err)
	}

	if foundIndex >= 0 {
		// Replace existing resource
		parsedData[foundIndex] = resourceDoc
	} else {
		// Append new resource
		parsedData = append(parsedData, resourceDoc)
	}

	return parsedData, nil, nil
}
