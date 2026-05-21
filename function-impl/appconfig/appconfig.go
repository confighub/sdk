// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

// Package appconfig registers functions that operate on AppConfig/* toolchain
// units. Currently the only function is render-configmap, which renders the
// AppConfig data as a Kubernetes ConfigMap YAML document.
package appconfig

import (
	"github.com/confighub/sdk/core/configkit"
	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/function/handler"
)

// RegisterFunctions registers the AppConfig functions on the given FunctionRegistry.
// It is intended to be called from each AppConfig toolchain's RegisterFunctions in
// addition to the standard generic function registration.
func RegisterFunctions(converter configkit.ConfigConverter, resourceProvider yamlkit.ResourceProvider, fh handler.FunctionRegistry) {
	registerRenderConfigMap(fh, converter, resourceProvider)
}
