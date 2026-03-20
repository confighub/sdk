// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package appjson

import (
	"github.com/confighub/sdk/configkit/jsonkit"
	"github.com/confighub/sdk/core/function/handler"
)

// RegisterFunctions registers all AppConfig JSON functions onto the provided FunctionHandler
// using the given registrar's resource provider.
func RegisterFunctions(rp *jsonkit.JSONResourceProviderType, fh handler.FunctionRegistry) {
	initStandardFunctions(rp)
	registerStandardFunctions(fh, rp)
}
