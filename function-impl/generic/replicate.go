// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package generic

import (
	"fmt"
	"log/slog"

	"github.com/confighub/sdk/core/configkit"
	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/function/handler"
	"github.com/confighub/sdk/core/third_party/gaby"
)

func registerReplicate(fh handler.FunctionRegistry, converter configkit.ConfigConverter, resourceProvider yamlkit.ResourceProvider) {
	if err := fh.RegisterFunction("replicate", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "replicate",
			Parameters: []api.FunctionParameter{
				{
					ParameterName: "resource-type",
					Required:      true,
					Description:   "Type (" + resourceProvider.TypeDescription() + ") of the resource/element to replicate",
					DataType:      api.DataTypeString,
				},
				{
					ParameterName: "resource-name",
					Required:      true,
					Description:   "Name of the resource/element to replicate",
					DataType:      api.DataTypeString,
				},
				{
					ParameterName: "replicas",
					Required:      true,
					Description:   "Desired number of replicas of the resource/element",
					DataType:      api.DataTypeInt,
				},
				{
					ParameterName: "resource-category",
					Required:      false,
					Description:   "Category of the resource/element to replicate",
					DataType:      api.DataTypeString,
				},
			},
			Mutating:              true,
			Validating:            false,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Replicate the specified configuration resource/element replicas minus one times",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
			return genericFnReplicate(resourceProvider, fArgs.Options, fArgs.FunctionContext, fArgs.ParsedData, fArgs.Arguments)
		},
	}); err != nil {
		slog.Error("failed to register function", "error", err)
	}
}

func genericFnReplicate(resourceProvider yamlkit.ResourceProvider, options *api.FunctionOptions, functionContext *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument) (gaby.Container, any, error) {
	matchResourceType := api.ResourceType(args[0].Value.(string))
	matchResourceName := api.ResourceName(args[1].Value.(string))
	replicas := args[2].Value.(int)
	var matchResourceCategory api.ResourceCategory
	if len(args) > 3 {
		matchResourceCategory = api.ResourceCategory(args[3].Value.(string))
	} else {
		// Default category.
		matchResourceCategory = resourceProvider.DefaultResourceCategory()
	}

	type replicateMatch struct {
		index        int
		resourceName api.ResourceName
	}

	output, err := yamlkit.VisitResourcesFiltered(parsedData, nil, resourceProvider, options, func(doc *gaby.YamlDoc, output any, index int, resourceInfo *api.ResourceInfo) (any, []error) {
		if output != nil {
			return output, nil // Already found a match
		}
		if resourceInfo.ResourceCategory != matchResourceCategory ||
			resourceInfo.ResourceType != matchResourceType ||
			resourceInfo.ResourceNameWithoutScope != matchResourceName {
			return output, nil
		}
		return &replicateMatch{
			index:        index,
			resourceName: resourceInfo.ResourceNameWithoutScope,
		}, nil
	})
	if err != nil {
		return parsedData, nil, err
	}
	if output == nil {
		return parsedData, nil, nil
	}
	match := output.(*replicateMatch)

	// Replicate this resource by insertion
	i := match.index
	newParsedData := make(gaby.Container, len(parsedData)+replicas-1)
	for j := 0; j < i; j++ {
		newParsedData[j] = parsedData[j]
	}
	for j := 0; j < replicas; j++ {
		replicatedResource := parsedData[i].Bytes()
		parsedReplicatedResource, err := gaby.ParseYAML(replicatedResource)
		if err != nil {
			return parsedData, nil, err
		}
		// TODO: This uniquifies the resource name, but not other attributes in the resource, if required.
		_ = resourceProvider.SetResourceName(parsedReplicatedResource, fmt.Sprintf("%s%d", string(match.resourceName), j))
		// Delete the resource merge ID so the replicated resource gets a new unique ID.
		_ = resourceProvider.DeleteResourceMergeID(parsedReplicatedResource)
		newParsedData[i+j] = parsedReplicatedResource
	}
	for j := i + 1; j < len(parsedData); j++ {
		newParsedData[j+replicas-1] = parsedData[j]
	}
	return newParsedData, nil, nil
}
