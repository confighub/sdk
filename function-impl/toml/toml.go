// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package toml

import (
	"github.com/confighub/sdk/configkit/tomlkit"
	"github.com/confighub/sdk/function/api"
	"github.com/confighub/sdk/function/handler"
	"github.com/confighub/sdk/workerapi"
)

type TOMLRegistrarType struct {
	resourceProvider *tomlkit.TOMLResourceProviderType
}

// NewTOMLRegistrar creates a new TOMLRegistrarType with its own resource provider.
func NewTOMLRegistrar() *TOMLRegistrarType {
	return &TOMLRegistrarType{resourceProvider: tomlkit.NewTOMLResourceProvider()}
}

func (r *TOMLRegistrarType) RegisterFunctions(fh handler.FunctionRegistry) {
	initStandardFunctions(r.resourceProvider)
	registerStandardFunctions(fh, r.resourceProvider)
	fh.SetConverter(r.resourceProvider)
	fh.SetResourceProvider(r.resourceProvider)
}

func (r *TOMLRegistrarType) GetToolchainPath() string {
	return api.SupportedToolchains[workerapi.ToolchainAppConfigTOML]
}

func (r *TOMLRegistrarType) SetPathRegistry(fh handler.FunctionRegistry) {
	fh.SetPathRegistry(r.resourceProvider.GetPathRegistry())
}
