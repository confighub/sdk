// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package generic

import (
	"encoding/json"

	"github.com/confighub/sdk/core/configkit"
	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/function/handler"
	"github.com/confighub/sdk/core/third_party/gaby"
)

func registerGetNeeded(fh handler.FunctionRegistry, converter configkit.ConfigConverter, resourceProvider yamlkit.ResourceProvider) {
	fh.RegisterFunction("get-needed", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "get-needed",
			OutputInfo: &api.FunctionOutput{
				ResultName:  "attribute-list",
				Description: "Needed attributes",
				OutputType:  api.OutputTypeAttributeValueList,
				Schema:      &api.AttributeValueListSchema,
			},
			Mutating:              false,
			Validating:            false,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Returns a list of needed attributes with setter functions. See https://docs.confighub.com/background/concepts/needsprovides/ for more information.",
			FunctionType:          api.FunctionTypePathVisitor,
			AttributeName:         api.AttributeNameNone,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
			var whereExpressions []*api.VisitorRelationalExpression
			if fArgs.Options != nil {
				whereExpressions = fArgs.Options.WhereResourceExpressions
			}
			return genericFnGetNeeded(resourceProvider, fArgs.FunctionContext, fArgs.ParsedData, fArgs.Arguments, whereExpressions)
		},
	})
}

func genericFnGetNeeded(resourceProvider yamlkit.ResourceProvider, _ *api.FunctionContext, parsedData gaby.Container, _ []api.FunctionArgument, whereExpressions []*api.VisitorRelationalExpression) (gaby.Container, any, error) {
	values, err := yamlkit.GetRegisteredNeededStringPaths(parsedData, resourceProvider, whereExpressions)
	// TODO: int, bool
	return parsedData, values, err
}

func registerGetProvided(fh handler.FunctionRegistry, converter configkit.ConfigConverter, resourceProvider yamlkit.ResourceProvider) {
	fh.RegisterFunction("get-provided", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "get-provided",
			OutputInfo: &api.FunctionOutput{
				ResultName:  "attribute-list",
				Description: "Provided attributes",
				OutputType:  api.OutputTypeAttributeValueList,
				Schema:      &api.AttributeValueListSchema,
			},
			Mutating:              false,
			Validating:            false,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Returns a list of Provided attributes. See https://docs.confighub.com/background/concepts/needsprovides/ for more information.",
			FunctionType:          api.FunctionTypePathVisitor,
			AttributeName:         api.AttributeNameNone,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
			var whereExpressions []*api.VisitorRelationalExpression
			if fArgs.Options != nil {
				whereExpressions = fArgs.Options.WhereResourceExpressions
			}
			return genericFnGetProvided(resourceProvider, fArgs.FunctionContext, fArgs.ParsedData, fArgs.Arguments, whereExpressions)
		},
	})
}

func genericFnGetProvided(resourceProvider yamlkit.ResourceProvider, _ *api.FunctionContext, parsedData gaby.Container, _ []api.FunctionArgument, whereExpressions []*api.VisitorRelationalExpression) (gaby.Container, any, error) {
	values, err := yamlkit.GetRegisteredProvidedStringPaths(parsedData, resourceProvider, whereExpressions)
	// TODO: int, bool
	return parsedData, values, err
}

func registerGetPaths(fh handler.FunctionRegistry, converter configkit.ConfigConverter, resourceProvider yamlkit.ResourceProvider) {
	fh.RegisterFunction("get-paths", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "get-paths",
			Parameters: []api.FunctionParameter{
				{
					ParameterName: "paths",
					Required:      true,
					Description:   "JSON-serialized list of AttributeInfo objects identifying the paths to fetch values for",
					DataType:      api.DataTypeAttributeInfoList,
				},
			},
			OutputInfo: &api.FunctionOutput{
				ResultName:  "attribute-list",
				Description: "Attribute values at the specified paths",
				OutputType:  api.OutputTypeAttributeValueList,
				Schema:      &api.AttributeValueListSchema,
			},
			Mutating:              false,
			Validating:            false,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Returns the current values at the specified attribute paths. Used to fetch values for stored NeededPaths and ProvidedPaths.",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
			var whereExpressions []*api.VisitorRelationalExpression
			if fArgs.Options != nil {
				whereExpressions = fArgs.Options.WhereResourceExpressions
			}
			return genericFnGetPaths(resourceProvider, fArgs.ParsedData, fArgs.Arguments, whereExpressions)
		},
	})
}

// enrichMergeKeysFromPath extracts merge keys from a resolved path (with numeric indices)
// and adds them as NeededPreferred properties on the attribute's Details. For example,
// path "spec.template.spec.volumes.1.configMap.name" with merge key "name"="config" at
// volumes[1] yields NeededPreferred["Name"] = "config".
func enrichMergeKeysFromPath(parsedData gaby.Container, resourceProvider yamlkit.ResourceProvider, attr *api.AttributeValue) {
	doc, _ := yamlkit.FindResourceDoc(parsedData, resourceProvider, &attr.ResourceInfo)
	if doc == nil {
		return
	}
	yamlkit.EnrichMergeKeysFromDoc(doc, resourceProvider, attr)
}

func genericFnGetPaths(resourceProvider yamlkit.ResourceProvider, parsedData gaby.Container, args []api.FunctionArgument, whereExpressions []*api.VisitorRelationalExpression) (gaby.Container, any, error) {
	attributeInfoJSON := args[0].Value.(string)
	var attributeInfoList []api.AttributeInfo
	if err := json.Unmarshal([]byte(attributeInfoJSON), &attributeInfoList); err != nil {
		return parsedData, nil, err
	}

	// Get all requested paths even if not in the path registry
	var allValues api.AttributeValueList
	for _, info := range attributeInfoList {
		resourceType := info.ResourceType
		if resourceType == "" {
			resourceType = api.ResourceTypeAny
		}
		resourceTypeToPaths := yamlkit.GetVisitorMapForPath(resourceProvider, resourceType, api.UnresolvedPath(info.Path))
		values, err := yamlkit.GetPathsAnyType(parsedData, resourceTypeToPaths, []any{}, resourceProvider, info.DataType, false, false, whereExpressions)
		if err != nil {
			return parsedData, nil, err
		}
		// Preserve the original AttributeInfo metadata on each returned value
		isNeeded := info.Details != nil && info.Details.IsNeeded
		isProvided := info.Details != nil && info.Details.IsProvided
		for i := range values {
			if values[i].AttributeName == api.AttributeNameNone || values[i].AttributeName == "" {
				values[i].AttributeName = info.AttributeName
			}
			if values[i].DataType == api.DataTypeNone || values[i].DataType == "" {
				values[i].DataType = info.DataType
			}
			// Use the stored Details from the input AttributeInfo. The stored
			// Details came from get-needed/get-provided and have the correct
			// getter/setter invocations with proper arguments. The visitor may
			// produce different Details from generic path registrations that
			// don't match the needed/provided context.
			if info.Details != nil {
				values[i].Details = info.Details
			}
			// For needed paths, extract merge keys as NeededPreferred properties.
			if isNeeded {
				enrichMergeKeysFromPath(parsedData, resourceProvider, &values[i])
			}
			// Invoke the Enricher from the PathVisitorInfo if the path is registered.
			visitorInfo := yamlkit.GetPathVisitorInfo(resourceProvider, info.ResourceType, api.UnresolvedPath(info.Path))
			if visitorInfo != nil {
				if enricher, ok := visitorInfo.Enricher.(yamlkit.AttributeEnricher); ok && enricher != nil {
					doc, _ := yamlkit.FindResourceDoc(parsedData, resourceProvider, &values[i].ResourceInfo)
					if doc != nil {
						_ = enricher(doc, &values[i], isProvided)
					}
				}
			}
		}
		allValues = append(allValues, values...)
	}
	return parsedData, allValues, nil
}
