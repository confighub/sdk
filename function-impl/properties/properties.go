// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package properties

import (
	"github.com/confighub/sdk/configkit/propkit"
	"github.com/confighub/sdk/function/api"
	"github.com/confighub/sdk/function/handler"
	"github.com/confighub/sdk/workerapi"
)

type PropertiesRegistrarType struct {
	resourceProvider *propkit.PropertiesResourceProviderType
}

// NewPropertiesRegistrar creates a new PropertiesRegistrarType with its own resource provider.
func NewPropertiesRegistrar() *PropertiesRegistrarType {
	return &PropertiesRegistrarType{resourceProvider: propkit.NewPropertiesResourceProvider()}
}

func (r *PropertiesRegistrarType) RegisterFunctions(fh handler.FunctionRegistry) {
	initStandardFunctions(r.resourceProvider)
	registerStandardFunctions(fh, r.resourceProvider)
	fh.SetConverter(r.resourceProvider)
	fh.SetResourceProvider(r.resourceProvider)
}

func (r *PropertiesRegistrarType) GetToolchainPath() string {
	return api.SupportedToolchains[workerapi.ToolchainAppConfigProperties]
}

func (r *PropertiesRegistrarType) SetPathRegistry(fh handler.FunctionRegistry) {
	fh.SetPathRegistry(r.resourceProvider.GetPathRegistry())
}
