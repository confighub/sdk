// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package confighub

import (
	"github.com/confighub/sdk/configkit/cubkit"
	"github.com/confighub/sdk/configkit/yamlkit"
	"github.com/confighub/sdk/function/api"
	"github.com/confighub/sdk/function/handler"
	"github.com/confighub/sdk/function/internal/handlers/generic"
)

// TODO: refactor to share code that's common across ToolchainTypes

func registerStandardFunctions(fh handler.FunctionRegistry) {
	generic.RegisterStandardFunctions(fh, cubkit.ConfigHubResourceProvider, cubkit.ConfigHubResourceProvider)
}

func initStandardFunctions() {
	// In general we don't recommend changing names of configs since names are used for identifying
	// configs across mutations, but it is necessary for "container" resources like Spaces.
	var defaultNames = api.ResourceTypeToPathToVisitorInfoType{
		api.ResourceTypeAny: {
			api.UnresolvedPath(cubkit.ConfigHubResourceProvider.ScopelessResourceNamePath()): {
				Path:          api.UnresolvedPath(cubkit.ConfigHubResourceProvider.ScopelessResourceNamePath()),
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
			cubkit.ConfigHubResourceProvider,
			api.AttributeNameDefaultName,
			resourceType,
			pathInfos,
			nil,
			setterFunctionInvocation,
			false,
		)
		yamlkit.RegisterPathsByAttributeName(
			cubkit.ConfigHubResourceProvider,
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
			cubkit.ConfigHubResourceProvider,
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
			cubkit.ConfigHubResourceProvider,
			api.AttributeNameDetail,
			resourceType,
			pathInfos,
			nil,
			nil,
			false,
		)
	}

	// TODO
	// yamlkit.RegisterNeededPaths(cubkit.ConfigHubResourceProvider, api.ResourceTypeAny, pathInfos, setterFunctionInvocation)
}
