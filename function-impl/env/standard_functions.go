// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package env

import (
	"log/slog"

	"github.com/confighub/sdk/configkit/envkit"
	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/function/handler"
	"github.com/confighub/sdk/function-impl/generic"
)

// TODO: refactor to share code that's common across ToolchainTypes

func registerStandardFunctions(fh handler.FunctionRegistry, rp *envkit.EnvResourceProviderType) {
	generic.RegisterStandardFunctions(fh, rp, rp)

	// Env only supports string types, so unregister int and bool functions
	if err := fh.RegisterFunction("get-bool-path", nil); err != nil {
		slog.Error("failed to unregister function", "error", err)
	}
	if err := fh.RegisterFunction("set-bool-path", nil); err != nil {
		slog.Error("failed to unregister function", "error", err)
	}
	if err := fh.RegisterFunction("get-int-path", nil); err != nil {
		slog.Error("failed to unregister function", "error", err)
	}
	if err := fh.RegisterFunction("set-int-path", nil); err != nil {
		slog.Error("failed to unregister function", "error", err)
	}
}

func initStandardFunctions(rp *envkit.EnvResourceProviderType) {
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
			false, false,
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
			false, false,
		)
	}
}
