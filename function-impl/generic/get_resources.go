// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package generic

import (
	"log/slog"
	"strings"

	"github.com/confighub/sdk/core/configkit"
	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/function/handler"
	"github.com/confighub/sdk/core/third_party/gaby"
)

func registerGetResources(fh handler.FunctionRegistry, converter configkit.ConfigConverter, resourceProvider yamlkit.ResourceProvider) {
	if err := fh.RegisterFunction("get-resources", &handler.FunctionRegistration{
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
				Schema:      &api.ResourceListSchema,
			},
			Mutating:              false,
			Validating:            false,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Returns a list of resources and their types",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
			return genericFnGetResources(converter, resourceProvider, fArgs.Options, fArgs.FunctionContext, fArgs.ParsedData, fArgs.Arguments)
		},
	}); err != nil {
		slog.Error("failed to register function", "error", err)
	}
}

func genericFnGetResources(converter configkit.ConfigConverter, resourceProvider yamlkit.ResourceProvider, options *api.FunctionOptions, _ *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument) (gaby.Container, any, error) {
	// Default body format is "yaml"
	bodyFormat := "yaml"
	if len(args) > 0 {
		bodyFormat = strings.ToLower(args[0].Value.(string))
	}

	list := make(api.ResourceList, 0, len(parsedData))
	_, err := yamlkit.VisitResourcesFiltered(parsedData, nil, resourceProvider, options,
		func(doc *gaby.YamlDoc, output any, _ int, resourceInfo *api.ResourceInfo) (any, []error) {
			var resourceBody string
			switch bodyFormat {
			case "none":
				resourceBody = ""
			case "json":
				jsonBytes, err := doc.MarshalJSON()
				if err != nil {
					return output, []error{err}
				}
				resourceBody = string(jsonBytes)
			case "native":
				yamlBytes := []byte(doc.String())
				nativeBytes, err := converter.YAMLToNative(yamlBytes)
				if err != nil {
					return output, []error{err}
				}
				resourceBody = string(nativeBytes)
			case "yaml":
				fallthrough
			default:
				resourceBody = doc.String()
			}

			list = append(list, api.Resource{
				ResourceInfo: *resourceInfo,
				ResourceBody: resourceBody,
			})
			return output, nil
		})
	if err != nil {
		return parsedData, nil, err
	}
	return parsedData, list, nil
}

func registerGetResourcesOfType(fh handler.FunctionRegistry, converter configkit.ConfigConverter, resourceProvider yamlkit.ResourceProvider) {
	if err := fh.RegisterFunction("get-resources-of-type", &handler.FunctionRegistration{
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
				Schema:      &api.ResourceInfoListSchema,
			},
			Mutating:              false,
			Validating:            false,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Returns a list of resources of the specified type",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
			return genericFnGetResourcesOfType(resourceProvider, fArgs.FunctionContext, fArgs.ParsedData, fArgs.Arguments, fArgs.Options)
		},
	}); err != nil {
		slog.Error("failed to register function", "error", err)
	}
}

func genericFnGetResourcesOfType(resourceProvider yamlkit.ResourceProvider, _ *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, options *api.FunctionOptions) (gaby.Container, any, error) {
	resourceType := api.ResourceType(args[0].Value.(string))
	var list api.ResourceInfoList
	_, err := yamlkit.VisitResourcesFiltered(parsedData, nil, resourceProvider, options,
		func(_ *gaby.YamlDoc, output any, _ int, resourceInfo *api.ResourceInfo) (any, []error) {
			if resourceInfo.ResourceType == resourceType {
				list = append(list, *resourceInfo)
			}
			return output, nil
		})
	if err != nil {
		return parsedData, nil, err
	}
	return parsedData, list, nil
}
