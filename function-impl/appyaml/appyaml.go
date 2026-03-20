// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package appyaml

import (
	"github.com/confighub/sdk/configkit/appyamlkit"
	"github.com/confighub/sdk/core/function/handler"
)

// RegisterFunctions registers all AppConfig YAML functions onto the provided FunctionHandler
// using the given registrar's resource provider.
func RegisterFunctions(rp *appyamlkit.AppConfigYAMLResourceProviderType, fh handler.FunctionRegistry) {
	initStandardFunctions(rp)
	registerStandardFunctions(fh, rp)
}
