// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package ini

import (
	"github.com/confighub/sdk/configkit/inikit"
	"github.com/confighub/sdk/function/handler"
)

// RegisterFunctions registers all INI functions onto the provided FunctionHandler
// using the given registrar's resource provider.
func RegisterFunctions(rp *inikit.INIResourceProviderType, fh handler.FunctionRegistry) {
	initStandardFunctions(rp)
	registerStandardFunctions(fh, rp)
}
