// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package confighub

import (
	"github.com/confighub/sdk/core/configkit/cubkit"
	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/function/handler"
)

// RegisterFunctions registers all ConfigHub functions onto the provided FunctionHandler
// using the given registrar's resource provider.
func RegisterFunctions(rp *cubkit.ConfigHubResourceProviderType, fh handler.FunctionRegistry) {
	initStandardFunctions(rp)
	registerDeclaredAttributePaths(rp)
	registerStandardFunctions(fh, rp)
}

// attributeDescriptors say what each attribute the ConfigHub/YAML specs declare is. The specs say
// where each one lives.
func attributeDescriptors() map[api.AttributeName]yamlkit.AttributeDescriptor {
	return map[api.AttributeName]yamlkit.AttributeDescriptor{
		// Nothing reads the value at an immutable path -- the attribute is a predicate on the
		// path, read by vet-immutable -- so it has no getter, no setter, and a data type that
		// admits anything.
		cubkit.AttributeNameImmutable: {DataType: api.DataTypeYAML},
	}
}

func registerDeclaredAttributePaths(rp *cubkit.ConfigHubResourceProviderType) {
	if err := cubkit.RegisterDeclaredAttributePaths(rp, attributeDescriptors()); err != nil {
		// The specs are embedded in cubkit and the descriptors are in this file, so a failure is a
		// mismatch between the two rather than anything a caller did.
		panic("confighub: registering declared attribute paths: " + err.Error())
	}
}
