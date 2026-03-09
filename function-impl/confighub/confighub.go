// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package confighub

import (
	"github.com/confighub/sdk/configkit/cubkit"
	"github.com/confighub/sdk/function/api"
	"github.com/confighub/sdk/function/handler"
	"github.com/confighub/sdk/workerapi"
)

type ConfigHubRegistrarType struct {
	resourceProvider *cubkit.ConfigHubResourceProviderType
}

// NewConfigHubRegistrar creates a new ConfigHubRegistrarType with its own resource provider.
func NewConfigHubRegistrar() *ConfigHubRegistrarType {
	return &ConfigHubRegistrarType{resourceProvider: cubkit.NewConfigHubResourceProvider()}
}

func (r *ConfigHubRegistrarType) RegisterFunctions(fh handler.FunctionRegistry) {
	initStandardFunctions(r.resourceProvider)
	registerStandardFunctions(fh, r.resourceProvider)
	fh.SetConverter(r.resourceProvider)
	fh.SetResourceProvider(r.resourceProvider)
}

func (r *ConfigHubRegistrarType) GetToolchainPath() string {
	return api.SupportedToolchains[workerapi.ToolchainConfigHubYAML]
}

func (r *ConfigHubRegistrarType) SetPathRegistry(fh handler.FunctionRegistry) {
	fh.SetPathRegistry(r.resourceProvider.GetPathRegistry())
}
