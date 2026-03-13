// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package opentofu

import (
	"github.com/confighub/sdk/configkit/hclkit"
	"github.com/confighub/sdk/function/handler"
)

// TODO: make extensible at the provider level

// RegisterFunctions registers all OpenTofu functions onto the provided FunctionHandler
// using the given registrar's resource provider.
func RegisterFunctions(rp *hclkit.HclResourceProviderType, fh handler.FunctionRegistry) {
	initStandardFunctions(rp)
	registerStandardFunctions(fh, rp)
}
