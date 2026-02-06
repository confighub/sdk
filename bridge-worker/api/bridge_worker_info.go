// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package api

import (
	"strings"

	"github.com/confighub/sdk/configkit/cubkit"
	"github.com/confighub/sdk/workerapi"
)

type BridgeWorkerInfo struct {
	SupportedConfigTypes []*ConfigType `json:",omitempty" description:"Configuration types of the bridges supported by the worker"`
}

type ProviderType string

type ConfigType struct {
	ProviderType     ProviderType            `swaggertype:"string" description:"Type identifying a bridge implementation supported by the worker"`
	ToolchainType    workerapi.ToolchainType `swaggertype:"string" description:"Configuration toolchain and format implemented by this bridge of the worker"`
	LiveStateType    workerapi.ToolchainType `json:",omitempty" swaggertype:"string" description:"Configuration toolchain and format of the LiveState for this bridge; required in order to invoke functions on LiveState"`
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
	ProviderConfigHub      ProviderType = "ConfigHub"
	ProviderKubernetes     ProviderType = "Kubernetes"
	ProviderFluxOCIWriter  ProviderType = "FluxOCIWriter"
	ProviderFluxRenderer   ProviderType = "FluxRenderer"
	ProviderConfigMap      ProviderType = "ConfigMap"
	ProviderOpenTofuAWS    ProviderType = "OpenTofu/AWS"
	ProviderArgoCDRenderer ProviderType = "ArgoCDRenderer"
)

var SupportedProviders = map[ProviderType]bool{
	ProviderConfigHub:      true,
	ProviderKubernetes:     true,
	ProviderFluxOCIWriter:  true,
	ProviderFluxRenderer:   true,
	ProviderConfigMap:      true,
	ProviderOpenTofuAWS:    true,
	ProviderArgoCDRenderer: true,
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
