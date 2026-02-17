// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package generic

import (
	"strings"

	"github.com/confighub/sdk/configkit"
	"github.com/confighub/sdk/configkit/yamlkit"
	"github.com/confighub/sdk/function/api"
	"github.com/confighub/sdk/function/handler"
	"github.com/confighub/sdk/third_party/gaby"
)

func registerGetResources(fh handler.FunctionRegistry, converter configkit.ConfigConverter, resourceProvider yamlkit.ResourceProvider) {
	fh.RegisterFunction("get-resources", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "get-resources",
			Parameters: []api.FunctionParameter{
				{
					ParameterName:    "body",
					Required:         false,
					Description:      "Format for resource body output: yaml (default), none, json, or native",
					DataType:         api.DataTypeEnum,
					Example:          "yaml",
					ValueConstraints: api.ValueConstraints{EnumValues: []string{"yaml", "none", "json", "native"}},
				},
			},
			OutputInfo: &api.FunctionOutput{
				ResultName:  "resource",
				Description: "Return the names, types, and bodies of the resources",
				OutputType:  api.OutputTypeResourceList,
				Schema:      &resourceListSchema,
			},
			Mutating:              false,
			Validating:            false,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Returns a list of resources and their types",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(functionContext *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument) (gaby.Container, any, error) {
			return genericFnGetResources(converter, resourceProvider, functionContext, parsedData, args)
		},
	})
}

func genericFnGetResources(converter configkit.ConfigConverter, resourceProvider yamlkit.ResourceProvider, _ *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument) (gaby.Container, any, error) {
	// Default body format is "yaml"
	bodyFormat := "yaml"
	if len(args) > 0 {
		bodyFormat = strings.ToLower(args[0].Value.(string))
	}

	list := make(api.ResourceList, 0, len(parsedData))
	for _, doc := range parsedData {
		resourceCategory, err := resourceProvider.ResourceCategoryGetter(doc)
		if err != nil {
			return parsedData, nil, err
		}
		resourceType, err := resourceProvider.ResourceTypeGetter(doc)
		if err != nil {
			return parsedData, nil, err
		}
		resourceName, err := resourceProvider.ResourceNameGetter(doc)
		if err != nil {
			return parsedData, nil, err
		}

		var resourceBody string
		switch bodyFormat {
		case "none":
			resourceBody = ""
		case "json":
			jsonBytes, err := doc.MarshalJSON()
			if err != nil {
				return parsedData, nil, err
			}
			resourceBody = string(jsonBytes)
		case "native":
			yamlBytes := []byte(doc.String())
			nativeBytes, err := converter.YAMLToNative(yamlBytes)
			if err != nil {
				return parsedData, nil, err
			}
			resourceBody = string(nativeBytes)
		case "yaml":
			fallthrough
		default:
			resourceBody = doc.String()
		}

		list = append(list, api.Resource{
			ResourceInfo: api.ResourceInfo{
				ResourceName:             resourceName,
				ResourceNameWithoutScope: resourceProvider.RemoveScopeFromResourceName(resourceName),
				ResourceType:             resourceType,
				ResourceCategory:         resourceCategory,
			},
			ResourceBody: resourceBody,
		})
	}
	return parsedData, list, nil
}

func registerGetResourcesOfType(fh handler.FunctionRegistry, converter configkit.ConfigConverter, resourceProvider yamlkit.ResourceProvider) {
	fh.RegisterFunction("get-resources-of-type", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "get-resources-of-type",
			Parameters: []api.FunctionParameter{
				{
					ParameterName: "resource-type",
					Required:      true,
					Description:   "Type (" + resourceProvider.TypeDescription() + ") of the resources to return",
					DataType:      api.DataTypeString,
				},
			},
			OutputInfo: &api.FunctionOutput{
				ResultName:  "resource-name",
				Description: "Return the names of resources of the specified type",
				OutputType:  api.OutputTypeResourceInfoList,
				Schema:      &resourceInfoListSchema,
			},
			Mutating:              false,
			Validating:            false,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Returns a list of resources of the specified type",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(functionContext *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument) (gaby.Container, any, error) {
			return genericFnGetResourcesOfType(resourceProvider, functionContext, parsedData, args)
		},
	})
}

func genericFnGetResourcesOfType(resourceProvider yamlkit.ResourceProvider, _ *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument) (gaby.Container, any, error) {
	resourceType := args[0].Value.(string)
	resourceMap, _, err := yamlkit.ResourceAndCategoryTypeMaps(parsedData, resourceProvider)
	if err != nil {
		return parsedData, nil, err
	}
	list := make(api.ResourceInfoList, 0, len(resourceMap))
	for resname, resCategoryTypes := range resourceMap {
		for _, resCategoryType := range resCategoryTypes {
			if resCategoryType.ResourceType == api.ResourceType(resourceType) {
				list = append(list, api.ResourceInfo{
					ResourceName:             resname,
					ResourceNameWithoutScope: resourceProvider.RemoveScopeFromResourceName(resname),
					ResourceType:             resCategoryType.ResourceType,
					ResourceCategory:         resCategoryType.ResourceCategory,
				})
			}
		}
	}
	return parsedData, list, nil
}
