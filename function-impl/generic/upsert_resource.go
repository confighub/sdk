// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package generic

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"

	"github.com/confighub/sdk/core/configkit"
	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/function/handler"
	"github.com/confighub/sdk/core/third_party/gaby"
)

func registerUpsertResource(fh handler.FunctionRegistry, converter configkit.ConfigConverter, resourceProvider yamlkit.ResourceProvider) {
	if err := fh.RegisterFunction("upsert-resource", &handler.FunctionRegistration{
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
			return genericFnUpsertResource(resourceProvider, fArgs.Options, fArgs.FunctionContext, fArgs.ParsedData, fArgs.Arguments)
		},
	}); err != nil {
		slog.Error("failed to register function", "error", err)
	}
}

func genericFnUpsertResource(resourceProvider yamlkit.ResourceProvider, options *api.FunctionOptions, functionContext *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument) (gaby.Container, any, error) {
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

	// Find the resource to upsert in the resource list, by name, or by name without the
	// namespace when that names one resource.
	target := api.ResourceInfo{ResourceType: targetResourceType, ResourceName: targetResourceName}
	listed := make([]api.ResourceInfo, len(resourceList))
	for i := range resourceList {
		listed[i] = resourceList[i].ResourceInfo
	}
	position, found := api.NewResourceIndex(resourceProvider, listed).Find(target)
	if !found {
		return parsedData, nil, fmt.Errorf("resource with type %s and name %s not found in resource-list", targetResourceType, targetResourceName)
	}

	return upsertResources(resourceProvider, options, parsedData, api.ResourceList{resourceList[position]})
}

func registerUpsertResources(fh handler.FunctionRegistry, converter configkit.ConfigConverter, resourceProvider yamlkit.ResourceProvider) {
	if err := fh.RegisterFunction("upsert-resources", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "upsert-resources",
			Parameters: []api.FunctionParameter{
				{
					ParameterName:    "resource-list",
					Required:         true,
					Description:      "ResourceList containing the resources to upsert",
					DataType:         api.DataTypeResourceList,
					ValueConstraints: api.ValueConstraints{Schema: &api.ResourceListSchema},
				},
			},
			Mutating:              true,
			Validating:            false,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Append each resource in the list that is not present in the configuration data and replace each one that is. Intended to be used with get-resources.",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
			var resourceList api.ResourceList
			if err := json.Unmarshal([]byte(fArgs.Arguments[0].Value.(string)), &resourceList); err != nil {
				return fArgs.ParsedData, nil, fmt.Errorf("failed to unmarshal resource-list argument: %v", err)
			}
			return upsertResources(resourceProvider, fArgs.Options, fArgs.ParsedData, resourceList)
		},
	}); err != nil {
		slog.Error("failed to register function", "error", err)
	}
}

// upsertResources replaces each resource of resourceList that parsedData has, among the
// resources options select, and appends each one it doesn't.
//
// A resource is found as api.ResourceIndex finds one: by type in any API version, and by name
// with its scope or, when that names one resource, without it. The list is the index's
// counterparts, so a name without its scope also has to name one resource of the list: a
// render of blue/app and green/app replaces neither with a lone red/app. That is what lets an
// upserted resource be found after a variant moved it to its own namespace, without letting
// two resources of one name in different namespaces be taken for each other.
func upsertResources(resourceProvider yamlkit.ResourceProvider, options *api.FunctionOptions, parsedData gaby.Container,
	resourceList api.ResourceList) (gaby.Container, any, error) {
	resources, positions, err := yamlkit.SelectedResources(parsedData, resourceProvider, options)
	if err != nil {
		return parsedData, nil, fmt.Errorf("failed to search for existing resources: %v", err)
	}
	listed := make([]api.ResourceInfo, len(resourceList))
	for i := range resourceList {
		listed[i] = resourceList[i].ResourceInfo
	}
	index := api.NewResourceIndex(resourceProvider, resources)
	index.Counterparts(listed)

	// The index is of the data as it was, so a resource the list names twice would be appended
	// twice rather than replaced the second time.
	seen := make(map[api.ResourceTypeAndName]bool, len(listed))
	for i := range listed {
		key := api.ResourceTypeAndFullNameFromResourceInfo(resourceProvider, listed[i])
		if seen[key] {
			return parsedData, nil, fmt.Errorf("resource-list has more than one resource with type %s and name %s",
				listed[i].ResourceType, listed[i].ResourceName)
		}
		seen[key] = true
	}

	result := slices.Clone(parsedData)
	for i := range resourceList {
		resourceDoc, err := gaby.ParseYAML([]byte(resourceList[i].ResourceBody))
		if err != nil {
			return parsedData, nil, fmt.Errorf("failed to parse the body of resource %s %s: %v",
				listed[i].ResourceType, listed[i].ResourceName, err)
		}
		match := index.Match(listed[i])
		if len(match.Ambiguous) > 0 {
			return parsedData, nil, fmt.Errorf("resource with type %s and name %s matches several resources in different scopes: %v",
				listed[i].ResourceType, listed[i].ResourceName, match.Ambiguous)
		}
		if match.Found {
			result[positions[match.Position]] = resourceDoc
		} else {
			result = append(result, resourceDoc)
		}
	}
	return result, nil, nil
}
