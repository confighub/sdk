// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package kubernetes

import (
	"github.com/confighub/sdk/configkit/k8skit"
	"github.com/confighub/sdk/function/api"
	"github.com/confighub/sdk/function/handler"
	"github.com/confighub/sdk/workerapi"
	"github.com/labstack/gommon/log"
)

type KubernetesRegistrarType struct{}

var KubernetesRegistrar = &KubernetesRegistrarType{}

func initFunctions() {
	err := InitSchemaFinder()
	if err != nil {
		log.Errorf("%v", err)
	}
	initMetadataFunctions()
	initStandardFunctions()
	initContainerFunctions()
}

func (r *KubernetesRegistrarType) RegisterFunctions(kh handler.FunctionRegistry) {
	initFunctions()

	registerStandardFunctions(kh)
	registerMetadataFunctions(kh)
	registerContainerFunctions(kh)

	kh.SetConverter(k8skit.K8sResourceProvider)
}

func (r *KubernetesRegistrarType) GetToolchainPath() string {
	return api.SupportedToolchains[workerapi.ToolchainKubernetesYAML]
}

func (r *KubernetesRegistrarType) SetPathRegistry(fh handler.FunctionRegistry) {
	fh.SetPathRegistry(k8skit.K8sResourceProvider.GetPathRegistry())
}
