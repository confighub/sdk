// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package text

import (
	"github.com/confighub/sdk/configkit/textkit"
	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/function/handler"
	"github.com/confighub/sdk/function-impl/generic"
)

// TODO: refactor to share code that's common across ToolchainTypes

func registerStandardFunctions(fh handler.FunctionRegistry, rp *textkit.TextResourceProviderType) {
	generic.RegisterStandardFunctions(fh, rp, rp)
}

// AttributeNameTextLine is the attribute name for individual text lines accessed via the Line embedded accessor.
const AttributeNameTextLine = api.AttributeName("text-line")

func initStandardFunctions(rp *textkit.TextResourceProviderType) {
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

	// Register the text field with a Line accessor so that individual lines
	// can be accessed via text#<line-number> paths (1-based).
	textLinePath := api.UnresolvedPath("text#%s")
	textLinePathInfo := &api.PathVisitorInfo{
		Path:                   textLinePath,
		AttributeName:          AttributeNameTextLine,
		DataType:               api.DataTypeString,
		EmbeddedAccessorType:   api.EmbeddedAccessorLine,
		EmbeddedAccessorConfig: "",
	}
	yamlkit.RegisterPathsByAttributeName(
		rp,
		AttributeNameTextLine,
		api.ResourceTypeAny,
		api.PathToVisitorInfoType{textLinePath: textLinePathInfo},
		nil,
		false, false,
	)
}
