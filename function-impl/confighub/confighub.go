// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package confighub

import (
	"github.com/confighub/sdk/configkit/cubkit"
	"github.com/confighub/sdk/function/handler"
)

// RegisterFunctions registers all ConfigHub functions onto the provided FunctionHandler
// using the given registrar's resource provider.
func RegisterFunctions(rp *cubkit.ConfigHubResourceProviderType, fh handler.FunctionRegistry) {
	initStandardFunctions(rp)
	registerStandardFunctions(fh, rp)
}
