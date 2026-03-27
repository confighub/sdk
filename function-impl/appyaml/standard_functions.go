// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package appyaml

import (
	"github.com/confighub/sdk/configkit/appyamlkit"
	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/function/handler"
	"github.com/confighub/sdk/function-impl/generic"
)

// TODO: refactor to share code that's common across ToolchainTypes

func registerStandardFunctions(fh handler.FunctionRegistry, rp *appyamlkit.AppConfigYAMLResourceProviderType) {
	generic.RegisterStandardFunctions(fh, rp, rp)
}

func initStandardFunctions(rp *appyamlkit.AppConfigYAMLResourceProviderType) {
	// In general we don't recommend changing names of configs since names are used for identifying
	// configs across mutations, but it is necessary for "container" resources.
	var defaultNames = api.ResourceTypeToPathToVisitorInfoType{
		api.ResourceTypeAny: {
			api.UnresolvedPath(rp.ScopelessResourceNamePath()): {
				Path:          api.UnresolvedPath(rp.ScopelessResourceNamePath()),
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
			rp,
			api.AttributeNameDefaultName,
			resourceType,
			pathInfos,
			&yamlkit.AttributeRegistrationDetails{SetterInvocation: setterFunctionInvocation},
		)
	}

	// TODO
	var detailPaths = api.ResourceTypeToPathToVisitorInfoType{}
	for resourceType, pathInfos := range detailPaths {
		yamlkit.RegisterPathsByAttributeName(
			rp,
			api.AttributeNameDetail,
			resourceType,
			pathInfos,
			nil,
		)
	}
}
