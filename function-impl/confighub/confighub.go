// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package confighub

import (
	"github.com/confighub/sdk/core/configkit/cubkit"
	"github.com/confighub/sdk/core/function/handler"
)

// RegisterFunctions registers all ConfigHub functions onto the provided FunctionHandler
// using the given registrar's resource provider.
func RegisterFunctions(rp *cubkit.ConfigHubResourceProviderType, fh handler.FunctionRegistry) {
	initStandardFunctions(rp)
	registerStandardFunctions(fh, rp)
}
