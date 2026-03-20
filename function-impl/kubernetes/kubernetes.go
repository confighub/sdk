// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package kubernetes

import (
	"log/slog"

	"github.com/confighub/sdk/configkit/k8skit"
	"github.com/confighub/sdk/core/function/handler"
)

func initFunctions(rp *k8skit.K8sResourceProviderType) {
	err := InitSchemaFinder()
	if err != nil {
		slog.Error("error", "error", err)
	}
	initMetadataFunctions(rp)
	initStandardFunctions(rp)
	initContainerFunctions(rp)
	initDefaultingFunctions(rp)
}

// RegisterFunctions registers all Kubernetes functions onto the provided FunctionHandler
// using the given registrar's resource provider.
func RegisterFunctions(rp *k8skit.K8sResourceProviderType, fh handler.FunctionRegistry) {
	initFunctions(rp)

	registerStandardFunctions(fh, rp)
	registerMetadataFunctions(fh, rp)
	registerContainerFunctions(fh, rp)
	registerDefaultingFunctions(fh, rp)
	registerK8sCELFunctions(fh, rp)
}
