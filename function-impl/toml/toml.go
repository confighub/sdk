// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package toml

import (
	"github.com/confighub/sdk/configkit/tomlkit"
	"github.com/confighub/sdk/core/function/handler"
)

// RegisterFunctions registers all TOML functions onto the provided FunctionHandler
// using the given registrar's resource provider.
func RegisterFunctions(rp *tomlkit.TOMLResourceProviderType, fh handler.FunctionRegistry) {
	initStandardFunctions(rp)
	registerStandardFunctions(fh, rp)
}
