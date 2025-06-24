// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package function

import (
	// It's generally better to register functions from their respective packages,
	// but for the worker which needs access across potential 'internal' boundaries,
	// we centralize the registration calls here.

	"github.com/confighub/sdk/function/internal/handlers/kubernetes"
	"github.com/confighub/sdk/function/internal/handlers/opentofu"
	"github.com/confighub/sdk/function/internal/handlers/properties"
	"github.com/confighub/sdk/function/handler"
)

// These are intended for use by components outside the main functions server, like workers,
// that need fully initialized handlers.

// RegisterKubernetes registers Kubernetes functions onto the provided FunctionHandler.
func RegisterKubernetes(fh *handler.FunctionHandler) {
	kubernetes.KubernetesRegistrar.RegisterFunctions(fh)
}

// RegisterProperties registers Properties functions onto the provided FunctionHandler.
func RegisterProperties(fh *handler.FunctionHandler) {
	properties.PropertiesRegistrar.RegisterFunctions(fh)
}

// RegisterOpenTofu registers OpenTofu functions onto the provided FunctionHandler.
func RegisterOpenTofu(fh *handler.FunctionHandler) {
	opentofu.OpenTofuRegistrar.RegisterFunctions(fh)
}
