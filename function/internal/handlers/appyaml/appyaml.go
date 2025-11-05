// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package appyaml

import (
	"github.com/confighub/sdk/configkit/appyamlkit"
	"github.com/confighub/sdk/function/api"
	"github.com/confighub/sdk/function/handler"
	"github.com/confighub/sdk/workerapi"
)

type AppConfigYAMLRegistrarType struct{}

var AppConfigYAMLRegistrar = &AppConfigYAMLRegistrarType{}

func (r *AppConfigYAMLRegistrarType) RegisterFunctions(fh handler.FunctionRegistry) {
	initStandardFunctions()
	registerStandardFunctions(fh)
	fh.SetConverter(appyamlkit.AppConfigYAMLResourceProvider)
	fh.SetResourceProvider(appyamlkit.AppConfigYAMLResourceProvider)
}

func (r *AppConfigYAMLRegistrarType) GetToolchainPath() string {
	return api.SupportedToolchains[workerapi.ToolchainAppConfigYAML]
}

func (r *AppConfigYAMLRegistrarType) SetPathRegistry(fh handler.FunctionRegistry) {
	fh.SetPathRegistry(appyamlkit.AppConfigYAMLResourceProvider.GetPathRegistry())
}
