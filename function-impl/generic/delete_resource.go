// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package generic

import (
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/confighub/sdk/core/configkit"
	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/function/handler"
	"github.com/confighub/sdk/core/third_party/gaby"
)

func registerDeleteResource(fh handler.FunctionRegistry, converter configkit.ConfigConverter, resourceProvider yamlkit.ResourceProvider) {
	if err := fh.RegisterFunction("delete-resource", &handler.FunctionRegistration{
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
			Idempotent:            true,
			Description:           "Remove the specified resource from the configuration data, if it is present",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
			return genericFnDeleteResource(resourceProvider, fArgs.Options, fArgs.FunctionContext, fArgs.ParsedData, fArgs.Arguments)
		},
	}); err != nil {
		slog.Error("failed to register function", "error", err)
	}
}

func genericFnDeleteResource(resourceProvider yamlkit.ResourceProvider, options *api.FunctionOptions, functionContext *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument) (gaby.Container, any, error) {
	return deleteResources(resourceProvider, options, parsedData, api.ResourceInfoList{{
		ResourceType: api.ResourceType(args[0].Value.(string)),
		ResourceName: api.ResourceName(args[1].Value.(string)),
	}})
}

func registerDeleteResources(fh handler.FunctionRegistry, converter configkit.ConfigConverter, resourceProvider yamlkit.ResourceProvider) {
	if err := fh.RegisterFunction("delete-resources", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "delete-resources",
			Parameters: []api.FunctionParameter{
				{
					ParameterName:    "resource-info-list",
					Required:         true,
					Description:      "ResourceInfoList naming the resources to delete",
					DataType:         api.DataTypeResourceInfoList,
					ValueConstraints: api.ValueConstraints{Schema: &api.ResourceInfoListSchema},
				},
			},
			Mutating:              true,
			Validating:            false,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Remove the specified resources that are present from the configuration data",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
			var resourceInfoList api.ResourceInfoList
			if err := json.Unmarshal([]byte(fArgs.Arguments[0].Value.(string)), &resourceInfoList); err != nil {
				return fArgs.ParsedData, nil, fmt.Errorf("failed to unmarshal resource-info-list argument: %v", err)
			}
			return deleteResources(resourceProvider, fArgs.Options, fArgs.ParsedData, resourceInfoList)
		},
	}); err != nil {
		slog.Error("failed to register function", "error", err)
	}
}

// deleteResources removes each resource of resourceInfoList that parsedData has, among the
// resources options select. A resource that is not there is already deleted, so it is not an
// error, and deleting again changes nothing.
//
// A resource is found as upsertResources finds one: by name, or by name without its scope when
// that names one resource both in parsedData and in the list, so that a stored invocation still
// finds a resource a clone has moved.
func deleteResources(resourceProvider yamlkit.ResourceProvider, options *api.FunctionOptions, parsedData gaby.Container,
	resourceInfoList api.ResourceInfoList) (gaby.Container, any, error) {
	resources, positions, err := yamlkit.SelectedResources(parsedData, resourceProvider, options)
	if err != nil {
		return parsedData, nil, fmt.Errorf("failed to search for resources to delete: %v", err)
	}
	index := api.NewResourceIndex(resourceProvider, resources)
	index.Counterparts(resourceInfoList)

	deleted := make(map[int]bool, len(resourceInfoList))
	for i := range resourceInfoList {
		match := index.Match(resourceInfoList[i])
		if len(match.Ambiguous) > 0 {
			return parsedData, nil, fmt.Errorf("resource with type %s and name %s matches several resources in different scopes: %v",
				resourceInfoList[i].ResourceType, resourceInfoList[i].ResourceName, match.Ambiguous)
		}
		if match.Found {
			deleted[positions[match.Position]] = true
		}
	}
	if len(deleted) == 0 {
		return parsedData, nil, nil
	}

	result := make(gaby.Container, 0, len(parsedData)-len(deleted))
	for i := range parsedData {
		if !deleted[i] {
			result = append(result, parsedData[i])
		}
	}
	return result, nil, nil
}
