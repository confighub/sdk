// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package impl

import (
	// It's generally better to register functions from their respective packages,
	// but for the worker which needs access across potential 'internal' boundaries,
	// we centralize the registration calls here.

	"github.com/confighub/sdk/function/handler"
	"github.com/confighub/sdk/function-impl/appyaml"
	"github.com/confighub/sdk/function-impl/confighub"
	"github.com/confighub/sdk/function-impl/ini"
	"github.com/confighub/sdk/function-impl/kubernetes"
	"github.com/confighub/sdk/function-impl/opentofu"
	"github.com/confighub/sdk/function-impl/properties"
	"github.com/confighub/sdk/function-impl/toml"
)

// These are intended for use by components outside the main functions server, like workers,
// that need fully initialized handlers.

// RegisterConfigHub registers ConfigHub functions onto the provided FunctionHandler.
func RegisterConfigHub(fh *handler.FunctionHandler) {
	confighub.NewConfigHubRegistrar().RegisterFunctions(fh)
}

// RegisterKubernetes registers Kubernetes functions onto the provided FunctionHandler.
func RegisterKubernetes(fh *handler.FunctionHandler) {
	kubernetes.NewKubernetesRegistrar().RegisterFunctions(fh)
}

// RegisterProperties registers Properties functions onto the provided FunctionHandler.
func RegisterProperties(fh *handler.FunctionHandler) {
	properties.NewPropertiesRegistrar().RegisterFunctions(fh)
}

// RegisterOpenTofu registers OpenTofu functions onto the provided FunctionHandler.
func RegisterOpenTofu(fh *handler.FunctionHandler) {
	opentofu.NewOpenTofuRegistrar().RegisterFunctions(fh)
}

// RegisterAppConfigYAML registers AppConfigYAML functions onto the provided FunctionHandler.
func RegisterAppConfigYAML(fh *handler.FunctionHandler) {
	appyaml.NewAppConfigYAMLRegistrar().RegisterFunctions(fh)
}

// RegisterTOML registers TOML functions onto the provided FunctionHandler.
func RegisterTOML(fh *handler.FunctionHandler) {
	toml.NewTOMLRegistrar().RegisterFunctions(fh)
}

// RegisterINI registers INI functions onto the provided FunctionHandler.
func RegisterINI(fh *handler.FunctionHandler) {
	ini.NewINIRegistrar().RegisterFunctions(fh)
}
