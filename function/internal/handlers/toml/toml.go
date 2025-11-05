// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package toml

import (
	"github.com/confighub/sdk/configkit/tomlkit"
	"github.com/confighub/sdk/function/api"
	"github.com/confighub/sdk/function/handler"
	"github.com/confighub/sdk/workerapi"
)

type TOMLRegistrarType struct{}

var TOMLRegistrar = &TOMLRegistrarType{}

func (r *TOMLRegistrarType) RegisterFunctions(fh handler.FunctionRegistry) {
	initStandardFunctions()
	registerStandardFunctions(fh)
	fh.SetConverter(tomlkit.TOMLResourceProvider)
	fh.SetResourceProvider(tomlkit.TOMLResourceProvider)
}

func (r *TOMLRegistrarType) GetToolchainPath() string {
	return api.SupportedToolchains[workerapi.ToolchainAppConfigTOML]
}

func (r *TOMLRegistrarType) SetPathRegistry(fh handler.FunctionRegistry) {
	fh.SetPathRegistry(tomlkit.TOMLResourceProvider.GetPathRegistry())
}
