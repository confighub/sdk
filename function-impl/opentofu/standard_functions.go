// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package opentofu

import (
	"github.com/confighub/sdk/configkit/hclkit"
	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/function/handler"
	"github.com/confighub/sdk/function-impl/generic"
)

func registerStandardFunctions(fh handler.FunctionRegistry, rp *hclkit.HclResourceProviderType) {
	generic.RegisterStandardFunctions(fh, rp, rp)
}

func initStandardFunctions(rp *hclkit.HclResourceProviderType) {
	// In general we don't recommend changing names of resources since names are used for identifying
	// resources across mutations, but it is necessary for "container" resources.
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
	// yamlkit.RegisterNeededPaths(rp, api.ResourceTypeAny, pathInfos, setterFunctionInvocation)
}
