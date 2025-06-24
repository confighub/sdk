// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package opentofu

import (
	"github.com/confighub/sdk/configkit/hclkit"
	"github.com/confighub/sdk/function/api"
	"github.com/confighub/sdk/function/handler"
	"github.com/confighub/sdk/workerapi"
)

type OpenTofuRegistrarType struct{}

var OpenTofuRegistrar = &OpenTofuRegistrarType{}

// TODO: make extensible at the provider level

func (r *OpenTofuRegistrarType) RegisterFunctions(fh handler.FunctionRegistry) {
	initStandardFunctions()
	registerStandardFunctions(fh)
	fh.SetConverter(hclkit.HclResourceProvider)
}

func (r *OpenTofuRegistrarType) GetToolchainPath() string {
	return api.SupportedToolchains[workerapi.ToolchainOpenTofuHCL]
}

func (r *OpenTofuRegistrarType) SetPathRegistry(fh handler.FunctionRegistry) {
	fh.SetPathRegistry(hclkit.HclResourceProvider.GetPathRegistry())
}
