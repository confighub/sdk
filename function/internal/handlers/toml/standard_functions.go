// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package toml

import (
	"github.com/confighub/sdk/configkit/tomlkit"
	"github.com/confighub/sdk/configkit/yamlkit"
	"github.com/confighub/sdk/function/api"
	"github.com/confighub/sdk/function/handler"
	"github.com/confighub/sdk/function/internal/handlers/generic"
)

// TODO: refactor to share code that's common across ToolchainTypes

func registerStandardFunctions(fh handler.FunctionRegistry) {
	generic.RegisterStandardFunctions(fh, tomlkit.TOMLResourceProvider, tomlkit.TOMLResourceProvider)
}

// This is also defined in the bridge.
const NamespaceProperty = "configHub.kubernetes.namespace"

func initStandardFunctions() {
	// In general we don't recommend changing names of configs since names are used for identifying
	// configs across mutations, but it is necessary for "container" resources.
	var defaultNames = api.ResourceTypeToPathToVisitorInfoType{
		api.ResourceTypeAny: {
			api.UnresolvedPath(tomlkit.TOMLResourceProvider.ScopelessResourceNamePath()): {
				Path:          api.UnresolvedPath(tomlkit.TOMLResourceProvider.ScopelessResourceNamePath()),
				AttributeName: api.AttributeNameResourceName,
				DataType:      api.DataTypeString,
			},
		},
	}
	setterFunctionInvocation := &api.FunctionInvocation{
		FunctionName: "set-default-names",
	}
	for resourceType, pathInfos := range defaultNames {
		yamlkit.RegisterPathsByAttributeName(
			tomlkit.TOMLResourceProvider,
			api.AttributeNameDefaultName,
			resourceType,
			pathInfos,
			nil,
			setterFunctionInvocation,
			false,
		)
		yamlkit.RegisterPathsByAttributeName(
			tomlkit.TOMLResourceProvider,
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
			tomlkit.TOMLResourceProvider,
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
			tomlkit.TOMLResourceProvider,
			api.AttributeNameDetail,
			resourceType,
			pathInfos,
			nil,
			nil,
			false,
		)
	}

	path := api.UnresolvedPath(NamespaceProperty)
	pathInfos := api.PathToVisitorInfoType{
		path: {
			Path:          path,
			AttributeName: api.AttributeNameResourceName,
			DataType:      api.DataTypeString,
		},
	}
	// Function to set the value. The parameters are expected to match the corresponding
	// get function's parameters plus its result.
	setterFunctionInvocation = &api.FunctionInvocation{
		FunctionName: "set-references-of-type",
		Arguments:    []api.FunctionArgument{{ParameterName: "resource-type", Value: "v1/Namespace"}},
	}
	yamlkit.RegisterNeededPaths(tomlkit.TOMLResourceProvider, api.ResourceTypeAny, pathInfos, setterFunctionInvocation)

}
