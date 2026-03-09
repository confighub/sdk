// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package appyaml

import (
	"github.com/confighub/sdk/configkit/appyamlkit"
	"github.com/confighub/sdk/function/api"
	"github.com/confighub/sdk/function/handler"
	"github.com/confighub/sdk/workerapi"
)

type AppConfigYAMLRegistrarType struct {
	resourceProvider *appyamlkit.AppConfigYAMLResourceProviderType
}

// NewAppConfigYAMLRegistrar creates a new AppConfigYAMLRegistrarType with its own resource provider.
func NewAppConfigYAMLRegistrar() *AppConfigYAMLRegistrarType {
	return &AppConfigYAMLRegistrarType{resourceProvider: appyamlkit.NewAppConfigYAMLResourceProvider()}
}

func (r *AppConfigYAMLRegistrarType) RegisterFunctions(fh handler.FunctionRegistry) {
	initStandardFunctions(r.resourceProvider)
	registerStandardFunctions(fh, r.resourceProvider)
	fh.SetConverter(r.resourceProvider)
	fh.SetResourceProvider(r.resourceProvider)
}

func (r *AppConfigYAMLRegistrarType) GetToolchainPath() string {
	return api.SupportedToolchains[workerapi.ToolchainAppConfigYAML]
}

func (r *AppConfigYAMLRegistrarType) SetPathRegistry(fh handler.FunctionRegistry) {
	fh.SetPathRegistry(r.resourceProvider.GetPathRegistry())
}
