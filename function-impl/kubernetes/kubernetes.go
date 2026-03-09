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

type KubernetesRegistrarType struct {
	resourceProvider *k8skit.K8sResourceProviderType
}

// NewKubernetesRegistrar creates a new KubernetesRegistrarType with its own resource provider.
func NewKubernetesRegistrar() *KubernetesRegistrarType {
	return &KubernetesRegistrarType{resourceProvider: k8skit.NewK8sResourceProvider()}
}

func initFunctions(rp *k8skit.K8sResourceProviderType) {
	err := InitSchemaFinder()
	if err != nil {
		log.Errorf("%v", err)
	}
	initMetadataFunctions(rp)
	initStandardFunctions(rp)
	initContainerFunctions(rp)
	initDefaultingFunctions(rp)
}

func (r *KubernetesRegistrarType) RegisterFunctions(kh handler.FunctionRegistry) {
	initFunctions(r.resourceProvider)

	registerStandardFunctions(kh, r.resourceProvider)
	registerMetadataFunctions(kh, r.resourceProvider)
	registerContainerFunctions(kh, r.resourceProvider)
	registerDefaultingFunctions(kh, r.resourceProvider)
	registerK8sCELFunctions(kh, r.resourceProvider)

	kh.SetConverter(r.resourceProvider)
	kh.SetResourceProvider(r.resourceProvider)
}

func (r *KubernetesRegistrarType) GetToolchainPath() string {
	return api.SupportedToolchains[workerapi.ToolchainKubernetesYAML]
}

func (r *KubernetesRegistrarType) SetPathRegistry(fh handler.FunctionRegistry) {
	fh.SetPathRegistry(r.resourceProvider.GetPathRegistry())
}

// GetResourceProvider returns the registrar's resource provider.
func (r *KubernetesRegistrarType) GetResourceProvider() *k8skit.K8sResourceProviderType {
	return r.resourceProvider
}
