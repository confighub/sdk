// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package api

import (
	"github.com/confighub/sdk/workerapi"
)

type BridgeWorkerInfo struct {
	SupportedConfigTypes []*ConfigType `json:",omitempty" description:"Configuration types supported by the BridgeWorker"`
}

type ProviderType string

type ConfigType struct {
	ToolchainType    workerapi.ToolchainType `json:",omitempty" swaggertype:"string" description:"Configuration toolchain and format supported by the BridgeWorker"`
	ProviderType     ProviderType            `json:",omitempty" swaggertype:"string" description:"Provider subtype of the configuration toolchain supported by the BridgegWorker"`
	AvailableTargets []Target                `json:",omitempty" description:"Targets known by the BridgeWorker"`
}

type Target struct {
	Name string `json:",omitempty" description:"Used to set the Slug and DisplayName of the Target created in ConfigHub"`
	// TODO: this should be perhaps some version of OCIParams like in the FluxOCIParams struct
	Params map[string]interface{} `json:",omitempty" description:"Used to set the Parameters of the Target created in ConfigHub"`
}

// ProviderType corresponds to the service API and client implementation
// TODO: Revisit whether this makes sense
const (
	ProviderKubernetes    ProviderType = "Kubernetes"
	ProviderFluxOCIWriter ProviderType = "FluxOCIWriter"
	ProviderConfigMap     ProviderType = "ConfigMap"
	ProviderAWS           ProviderType = "AWS"
)
