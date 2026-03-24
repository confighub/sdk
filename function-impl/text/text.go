// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package text

import (
	"github.com/confighub/sdk/configkit/textkit"
	"github.com/confighub/sdk/core/function/handler"
)

// RegisterFunctions registers all Text functions onto the provided FunctionHandler
// using the given registrar's resource provider.
func RegisterFunctions(rp *textkit.TextResourceProviderType, fh handler.FunctionRegistry) {
	initStandardFunctions(rp)
	registerStandardFunctions(fh, rp)
}
