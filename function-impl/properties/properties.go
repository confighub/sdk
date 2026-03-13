// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package properties

import (
	"github.com/confighub/sdk/configkit/propkit"
	"github.com/confighub/sdk/function/handler"
)

// RegisterFunctions registers all Properties functions onto the provided FunctionHandler
// using the given registrar's resource provider.
func RegisterFunctions(rp *propkit.PropertiesResourceProviderType, fh handler.FunctionRegistry) {
	initStandardFunctions(rp)
	registerStandardFunctions(fh, rp)
}
