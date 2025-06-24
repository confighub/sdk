// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package opentofu

import (
	"github.com/confighub/sdk/configkit/hclkit"
	"github.com/confighub/sdk/configkit/yamlkit"
	"github.com/confighub/sdk/function/internal/handlers/generic"
	"github.com/confighub/sdk/function/api"
	"github.com/confighub/sdk/function/handler"
)

func registerStandardFunctions(fh handler.FunctionRegistry) {
	generic.RegisterStandardFunctions(fh, hclkit.HclResourceProvider, hclkit.HclResourceProvider)
}

func initStandardFunctions() {
	// In general we don't recommend changing names of resources since names are used for identifying
	// resources across mutations.
	basicNameTemplate := generic.StandardNameTemplate(hclkit.HclResourceProvider.NameSeparator())
	var defaultNames = api.ResourceTypeToPathToVisitorInfoType{
		api.ResourceTypeAny: {
			api.UnresolvedPath(hclkit.HclResourceProvider.ScopelessResourceNamePath()): {
				Path:          api.UnresolvedPath(hclkit.HclResourceProvider.ScopelessResourceNamePath()),
				AttributeName: api.AttributeNameResourceName,
				DataType:      api.DataTypeString,
				Info:          &api.AttributeDetails{GenerationTemplate: basicNameTemplate},
			},
		},
	}
	setterFunctionInvocation := &api.FunctionInvocation{
		FunctionName: "set-default-names",
	}
	for resourceType, pathInfos := range defaultNames {
		yamlkit.RegisterPathsByAttributeName(
			hclkit.HclResourceProvider,
			api.AttributeNameDefaultName,
			resourceType,
			pathInfos,
			nil,
			setterFunctionInvocation,
			false,
		)
		yamlkit.RegisterPathsByAttributeName(
			hclkit.HclResourceProvider,
			api.AttributeNameGeneral,
			resourceType,
			pathInfos,
			nil,
			setterFunctionInvocation,
			true,
		)
	}

	// TODO
	var attributePaths = api.ResourceTypeToPathToVisitorInfoType{}
	for resourceType, pathInfos := range attributePaths {
		yamlkit.RegisterPathsByAttributeName(
			hclkit.HclResourceProvider,
			api.AttributeNameGeneral,
			resourceType,
			pathInfos,
			nil,
			nil,
			true,
		)
	}

	// TODO
	var detailPaths = api.ResourceTypeToPathToVisitorInfoType{}
	for resourceType, pathInfos := range detailPaths {
		yamlkit.RegisterPathsByAttributeName(
			hclkit.HclResourceProvider,
			api.AttributeNameDetail,
			resourceType,
			pathInfos,
			nil,
			nil,
			false,
		)
	}

	// TODO:
	// path := api.UnresolvedPath()
	// pathInfos := api.PathToVisitorInfoType{
	// 	path: {
	// 		Path:          path,
	// 		AttributeName: api.AttributeNameResourceName,
	// 		DataType:      api.DataTypeString,
	// 	},
	// }
	// // Function to set the value. The parameters are expected to match the corresponding
	// // get function's parameters plus its result.
	// setterFunctionInvocation = &api.FunctionInvocation{
	// 	FunctionName: "set-references-of-type",
	// 	Arguments:    []api.FunctionArgument{{ParameterName: "resource-type", Value: ""}},
	// }
	// yamlkit.RegisterNeededPaths(hclkit.HclResourceProvider, api.ResourceTypeAny, pathInfos, setterFunctionInvocation)
}
