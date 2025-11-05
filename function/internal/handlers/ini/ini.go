// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package ini

import (
	"github.com/confighub/sdk/configkit/inikit"
	"github.com/confighub/sdk/function/api"
	"github.com/confighub/sdk/function/handler"
	"github.com/confighub/sdk/workerapi"
)

type INIRegistrarType struct{}

var INIRegistrar = &INIRegistrarType{}

func (r *INIRegistrarType) RegisterFunctions(fh handler.FunctionRegistry) {
	initStandardFunctions()
	registerStandardFunctions(fh)
	fh.SetConverter(inikit.INIResourceProvider)
	fh.SetResourceProvider(inikit.INIResourceProvider)
}

func (r *INIRegistrarType) GetToolchainPath() string {
	return api.SupportedToolchains[workerapi.ToolchainAppConfigINI]
}

func (r *INIRegistrarType) SetPathRegistry(fh handler.FunctionRegistry) {
	fh.SetPathRegistry(inikit.INIResourceProvider.GetPathRegistry())
}
