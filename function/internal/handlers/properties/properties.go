// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package properties

import (
	"github.com/confighub/sdk/configkit/propkit"
	"github.com/confighub/sdk/function/api"
	"github.com/confighub/sdk/function/handler"
	"github.com/confighub/sdk/workerapi"
)

type PropertiesRegistrarType struct{}

var PropertiesRegistrar = &PropertiesRegistrarType{}

func (r *PropertiesRegistrarType) RegisterFunctions(fh handler.FunctionRegistry) {
	initStandardFunctions()
	registerStandardFunctions(fh)
	fh.SetConverter(propkit.PropertiesResourceProvider)
}

func (r *PropertiesRegistrarType) GetToolchainPath() string {
	return api.SupportedToolchains[workerapi.ToolchainAppConfigProperties]
}

func (r *PropertiesRegistrarType) SetPathRegistry(fh handler.FunctionRegistry) {
	fh.SetPathRegistry(propkit.PropertiesResourceProvider.GetPathRegistry())
}
