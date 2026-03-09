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

func registerSearchReplace(fh handler.FunctionRegistry, converter configkit.ConfigConverter, resourceProvider yamlkit.ResourceProvider) {
	fh.RegisterFunction("search-replace", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "search-replace",
			Parameters: []api.FunctionParameter{
				{
					ParameterName: "search-value",
					Required:      true,
					Description:   "String value to search for",
					DataType:      api.DataTypeString,
				},
				{
					ParameterName: "replace-value",
					Required:      true,
					Description:   "String value to use as the replacement for search-value",
					DataType:      api.DataTypeString,
				},
			},
			Mutating:              true,
			Validating:            false,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Replace all instances of the search-value in all strings of all resource types with replace-value",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
			return genericFnSearchReplace(resourceProvider, fArgs.FunctionContext, fArgs.ParsedData, fArgs.Arguments)
		},
	})
}

func genericFnSearchReplace(resourceProvider yamlkit.ResourceProvider, functionContext *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument) (gaby.Container, any, error) {
	searchValue := args[0].Value.(string)
	replaceValue := args[1].Value.(string)

	attributeList := yamlkit.FindYAMLPathsByValue(parsedData, resourceProvider, searchValue)
	for i := range attributeList {
		existingValue := attributeList[i].Value.(string)
		attributeList[i].Value = strings.ReplaceAll(existingValue, searchValue, replaceValue)
	}

	return genericSetAttributesFromList(resourceProvider, functionContext, parsedData, attributeList)
}
