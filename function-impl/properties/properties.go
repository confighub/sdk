// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package properties

import (
	"github.com/confighub/sdk/configkit/propkit"
	"github.com/confighub/sdk/core/function/handler"
	"github.com/confighub/sdk/function-impl/appconfig"
)

// RegisterFunctions registers all Properties functions onto the provided FunctionHandler
// using the given registrar's resource provider.
func RegisterFunctions(rp *propkit.PropertiesResourceProviderType, fh handler.FunctionRegistry) {
	initStandardFunctions(rp)
	registerStandardFunctions(fh, rp)
	appconfig.RegisterFunctions(rp, rp, fh)
}
