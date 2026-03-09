// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package ini

import (
	"github.com/confighub/sdk/configkit/inikit"
	"github.com/confighub/sdk/function/api"
	"github.com/confighub/sdk/function/handler"
	"github.com/confighub/sdk/workerapi"
)

type INIRegistrarType struct {
	resourceProvider *inikit.INIResourceProviderType
}

// NewINIRegistrar creates a new INIRegistrarType with its own resource provider.
func NewINIRegistrar() *INIRegistrarType {
	return &INIRegistrarType{resourceProvider: inikit.NewINIResourceProvider()}
}

func (r *INIRegistrarType) RegisterFunctions(fh handler.FunctionRegistry) {
	initStandardFunctions(r.resourceProvider)
	registerStandardFunctions(fh, r.resourceProvider)
	fh.SetConverter(r.resourceProvider)
	fh.SetResourceProvider(r.resourceProvider)
}

func (r *INIRegistrarType) GetToolchainPath() string {
	return api.SupportedToolchains[workerapi.ToolchainAppConfigINI]
}

func (r *INIRegistrarType) SetPathRegistry(fh handler.FunctionRegistry) {
	fh.SetPathRegistry(r.resourceProvider.GetPathRegistry())
}
