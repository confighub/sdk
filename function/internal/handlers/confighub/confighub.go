// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package confighub

import (
	"github.com/confighub/sdk/configkit/cubkit"
	"github.com/confighub/sdk/function/api"
	"github.com/confighub/sdk/function/handler"
	"github.com/confighub/sdk/workerapi"
)

type ConfigHubRegistrarType struct{}

var ConfigHubRegistrar = &ConfigHubRegistrarType{}

func (r *ConfigHubRegistrarType) RegisterFunctions(fh handler.FunctionRegistry) {
	initStandardFunctions()
	registerStandardFunctions(fh)
	fh.SetConverter(cubkit.ConfigHubResourceProvider)
	fh.SetResourceProvider(cubkit.ConfigHubResourceProvider)
}

func (r *ConfigHubRegistrarType) GetToolchainPath() string {
	return api.SupportedToolchains[workerapi.ToolchainConfigHubYAML]
}

func (r *ConfigHubRegistrarType) SetPathRegistry(fh handler.FunctionRegistry) {
	fh.SetPathRegistry(cubkit.ConfigHubResourceProvider.GetPathRegistry())
}
