// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package opentofu

import (
	"github.com/confighub/sdk/configkit/hclkit"
	"github.com/confighub/sdk/function/api"
	"github.com/confighub/sdk/function/handler"
	"github.com/confighub/sdk/workerapi"
)

type OpenTofuRegistrarType struct {
	resourceProvider *hclkit.HclResourceProviderType
}

// NewOpenTofuRegistrar creates a new OpenTofuRegistrarType with its own resource provider.
func NewOpenTofuRegistrar() *OpenTofuRegistrarType {
	return &OpenTofuRegistrarType{resourceProvider: hclkit.NewHclResourceProvider()}
}

// TODO: make extensible at the provider level

func (r *OpenTofuRegistrarType) RegisterFunctions(fh handler.FunctionRegistry) {
	initStandardFunctions(r.resourceProvider)
	registerStandardFunctions(fh, r.resourceProvider)
	fh.SetConverter(r.resourceProvider)
	fh.SetResourceProvider(r.resourceProvider)
}

func (r *OpenTofuRegistrarType) GetToolchainPath() string {
	return api.SupportedToolchains[workerapi.ToolchainOpenTofuHCL]
}

func (r *OpenTofuRegistrarType) SetPathRegistry(fh handler.FunctionRegistry) {
	fh.SetPathRegistry(r.resourceProvider.GetPathRegistry())
}
