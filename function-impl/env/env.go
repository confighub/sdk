// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package env

import (
	"github.com/confighub/sdk/configkit/envkit"
	"github.com/confighub/sdk/core/function/handler"
	"github.com/confighub/sdk/function-impl/appconfig"
)

// RegisterFunctions registers all Env functions onto the provided FunctionHandler
// using the given registrar's resource provider.
func RegisterFunctions(rp *envkit.EnvResourceProviderType, fh handler.FunctionRegistry) {
	initStandardFunctions(rp)
	registerStandardFunctions(fh, rp)
	appconfig.RegisterFunctions(rp, rp, fh)
}
