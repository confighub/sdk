// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package env

import (
	"github.com/confighub/sdk/configkit/envkit"
	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/function/handler"
	"github.com/confighub/sdk/function-impl/generic"
)

func registerStandardFunctions(fh handler.FunctionRegistry, rp *envkit.EnvResourceProviderType) {
	// Env values are text; the int and bool functions read and write their text (see
	// yamlkit.TextValuedResourceProvider).
	generic.RegisterStandardFunctions(fh, rp, rp)
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
	for resourceType, pathInfos := range defaultNames {
		yamlkit.RegisterPathsByAttributeName(
			rp,
			api.AttributeNameDefaultName,
			resourceType,
			pathInfos,
			nil,
			false, false,
		)
	}
}
