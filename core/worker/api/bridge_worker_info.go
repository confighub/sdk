// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package api

import (
	"regexp"
	"strings"

	"github.com/cockroachdb/errors"

	"github.com/confighub/sdk/configkit/cubkit"
	funcapi "github.com/confighub/sdk/function/api"
	"github.com/confighub/sdk/workerapi"
)

type BridgeWorkerInfo struct {
	SupportedConfigTypes []*SupportedConfigType `json:",omitempty" description:"Configuration types of the bridges supported by the worker"`
}

type ProviderType string

type ConfigType struct {
	ProviderType  ProviderType            `swaggertype:"string" description:"Type identifying a bridge implementation supported by the worker"`
	ToolchainType workerapi.ToolchainType `swaggertype:"string" description:"Configuration toolchain and format implemented by this bridge of the worker"`
	LiveStateType workerapi.ToolchainType `json:",omitempty" swaggertype:"string" description:"Configuration toolchain and format of the LiveState for this bridge; required in order to invoke functions on LiveState"`
}

type ConfigTypeSignature struct {
	ConfigType
	Options []BridgeOption `json:",omitempty" description:"Supported bridge options"`
}

// BridgeOption specifies the option name, description, required vs optional, data type, and example.
// It is very similar to FunctionParameter and the two may be unified at some point.
type BridgeOption struct {
	Name        string           `description:"Name of the option in PascalCase"`
	Description string           `description:"Description of the option"`
	Required    bool             `description:"Whether the option is required"`
	DataType    funcapi.DataType `swaggertype:"string" description:"Data type of the option"`
	Example     string           `json:",omitempty" description:"Example value"`
	// TODO: ValueConstraints
}

// TODO: Validate these
const MaxBridgeHandleLength = 512
const MaxBridgeOptionNameLength = 128
const MaxBridgeDescriptionLength = 1024

const BridgeHandlePrefixRegexpString = "^[A-Za-z0-9]([\\-_\\./\\:A-Za-z0-9]*[A-Za-z0-9])?"
const BridgeOptionNamePrefixRegexpString = "^[A-Za-z0-9]([\\-_A-Za-z0-9]{0,127})?"

type SupportedConfigType struct {
	ConfigTypeSignature
	AvailableTargets []Target `json:",omitempty" description:"Targets known by the BridgeWorker. Optional."`
}

var bridgeOptionNameRegexp = regexp.MustCompile(BridgeOptionNamePrefixRegexpString + "$")

func (sct *SupportedConfigType) Validate() error {
	if !IsValidProviderType(sct.ProviderType) {
		return errors.Errorf("invalid provider type %s", string(sct.ProviderType))
	}
	if !funcapi.IsSupportedToolchain(sct.ToolchainType) {
		return errors.Errorf("unsupported toolchain type %s", string(sct.ToolchainType))
	}
	// LiveStateType is optional
	if sct.LiveStateType != "" && !funcapi.IsSupportedToolchain(sct.LiveStateType) {
		return errors.Errorf("invalid livestate type %s", string(sct.LiveStateType))
	}
	// Validate BridgeOptions
	seen := make(map[string]bool, len(sct.Options))
	for _, opt := range sct.Options {
		if opt.Name == "" {
			return errors.New("bridge option name must not be empty")
		}
		if len(opt.Name) > MaxBridgeOptionNameLength {
			return errors.Errorf("bridge option name %q exceeds max length %d", opt.Name, MaxBridgeOptionNameLength)
		}
		if !bridgeOptionNameRegexp.MatchString(opt.Name) {
			return errors.Errorf("bridge option name %q does not match required pattern", opt.Name)
		}
		if seen[opt.Name] {
			return errors.Errorf("duplicate bridge option name %q", opt.Name)
		}
		seen[opt.Name] = true
		if len(opt.Description) > MaxBridgeDescriptionLength {
			return errors.Errorf("bridge option %q description exceeds max length %d", opt.Name, MaxBridgeDescriptionLength)
		}
	}
	return nil
}

type Target struct {
	BridgeHandle string                 `json:",omitempty" description:"Identifier used by the Bridge to refer to discovered/enabled Target credentials and coordinates"`
	Name         string                 `json:",omitempty" description:"Used to set the Slug and DisplayName of the Target created in ConfigHub. Optional."`
	Params       map[string]interface{} `json:",omitempty" description:"Deprecated. Used to set the Parameters of the Target created in ConfigHub"`
}

// ScrubAvailableTargets prepares AvailableTargets for storage by keeping only
// targets that have a BridgeHandle and clearing their Name and Params fields.
// Targets without a BridgeHandle are removed entirely.
func (sct *SupportedConfigType) ScrubAvailableTargets() {
	scrubbed := make([]Target, 0, len(sct.AvailableTargets))
	for _, t := range sct.AvailableTargets {
		if t.BridgeHandle != "" {
			scrubbed = append(scrubbed, Target{BridgeHandle: t.BridgeHandle})
		}
	}
	sct.AvailableTargets = scrubbed
}

type DriftReconciliationMode string

const (
	DriftReconciliationModeOnDemand          DriftReconciliationMode = "OnDemand"
	DriftReconciliationModeContinuousApply   DriftReconciliationMode = "ContinuousApply"
	DriftReconciliationModeContinuousRefresh DriftReconciliationMode = "ContinuousRefresh"
)

func IsValidDriftReconciliationMode(mode DriftReconciliationMode) bool {
	switch mode {
	case DriftReconciliationModeOnDemand, DriftReconciliationModeContinuousApply, DriftReconciliationModeContinuousRefresh:
		return true
	}
	return false
}

// ProviderType identifies a bridge implementation, including the API, authentication method, behavior, etc.
// It is used by the worker's dispatcher to route to the selected bridge.
const (
	ProviderConfigHub         ProviderType = "ConfigHub"
	ProviderKubernetes        ProviderType = "Kubernetes"
	ProviderFluxOCIWriter     ProviderType = "FluxOCIWriter"
	ProviderFluxRenderer      ProviderType = "FluxRenderer"
	ProviderArgoCDRenderer    ProviderType = "ArgoCDRenderer"
	ProviderArgoCDOCI         ProviderType = "ArgoCDOCI"
	ProviderConfigMapRenderer ProviderType = "ConfigMapRenderer"
	ProviderOpenTofuAWS       ProviderType = "OpenTofu/AWS"
	ProviderNoop              ProviderType = "Noop"
)

var SupportedProviders = map[ProviderType]bool{
	ProviderConfigHub:         true,
	ProviderKubernetes:        true,
	ProviderFluxOCIWriter:     true,
	ProviderFluxRenderer:      true,
	ProviderArgoCDRenderer:    true,
	ProviderArgoCDOCI:         true,
	ProviderConfigMapRenderer: true,
	ProviderOpenTofuAWS:       true,
	ProviderNoop:              true,
}

func IsSupportedProvider(provider ProviderType) bool {
	return SupportedProviders[provider]
}

// Max length for 3P providers
const MaxProviderTypeLength = 128

func IsValidProviderType(provider ProviderType) bool {
	if IsSupportedProvider(provider) {
		return true
	}
	providerString := string(provider)
	return providerString != "" && len(providerString) <= MaxProviderTypeLength
}

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
	return workerSlug + "-" + strings.ToLower(cubkit.CubNormalizeName(nameSuffix))
}
