// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package api

import (
	"strings"

	"github.com/confighub/sdk/configkit/cubkit"
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
	ProviderConfigHub     ProviderType = "ConfigHub"
	ProviderKubernetes    ProviderType = "Kubernetes"
	ProviderFluxOCIWriter ProviderType = "FluxOCIWriter"
	ProviderConfigMap     ProviderType = "ConfigMap"
	ProviderOpenTofuAWS   ProviderType = "OpenTofu/AWS"
)

var SupportedProviders = map[ProviderType]bool{
	ProviderConfigHub:     true,
	ProviderKubernetes:    true,
	ProviderFluxOCIWriter: true,
	ProviderConfigMap:     true,
	ProviderOpenTofuAWS:   true,
}

func IsSupportedProvider(provider ProviderType) bool {
	return SupportedProviders[provider]
}

// Max length for 3P providers
const MaxProviderTypeLength = 128

// Max size of marshaled parameters
const MaxParametersLength = 16384

func GenerateTargetName(workerSlug string, provider ProviderType, toolchain workerapi.ToolchainType, suffix string) string {
	nameSuffix := string(provider)

	// Reduce overlap in the case that the provider and toolchain share common prefixes.
	toolchainPrefix, toolchainSuffix, _ := strings.Cut(string(toolchain), "/")
	if strings.HasPrefix(string(provider), toolchainPrefix) {
		if toolchainSuffix != "" {
			nameSuffix += "-" + toolchainSuffix
		}
	} else {
		nameSuffix += "-" + string(toolchain)
	}

	if suffix != "" {
		nameSuffix += "-" + suffix
	}
	// workerSlug should already obey ConfigHub name character restrictions
	// TODO: truncate length if necessary
	return workerSlug + "-" + strings.ToLower(cubkit.ConfigHubResourceProvider.NormalizeName(nameSuffix))
}
