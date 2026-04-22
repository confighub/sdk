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

func registerSearchReplace(fh handler.FunctionRegistry, converter configkit.ConfigConverter, resourceProvider yamlkit.ResourceProvider) {
	if err := fh.RegisterFunction("search-replace", &handler.FunctionRegistration{
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
			return genericFnSearchReplace(resourceProvider, fArgs.FunctionContext, fArgs.ParsedData, fArgs.Arguments, fArgs.Options)
		},
	}); err != nil {
		slog.Error("failed to register function", "error", err)
	}
}

func genericFnSearchReplace(resourceProvider yamlkit.ResourceProvider, functionContext *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, options *api.FunctionOptions) (gaby.Container, any, error) {
	searchValue := args[0].Value.(string)
	replaceValue := args[1].Value.(string)

	attributeList := yamlkit.FindYAMLPathsByValue(parsedData, resourceProvider, searchValue, options)
	for i := range attributeList {
		existingValue := attributeList[i].Value.(string)
		attributeList[i].Value = strings.ReplaceAll(existingValue, searchValue, replaceValue)
	}

	return genericSetAttributesFromList(resourceProvider, functionContext, parsedData, attributeList, options)
}
