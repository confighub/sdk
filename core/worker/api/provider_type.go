// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package api

// ProviderType says whether and how a Unit is delivered.
type ProviderType string

const (
	// ProviderConfigHub marks configuration that ConfigHub applies to itself, such as the
	// ConfigHub/YAML Unit that holds an entity's configuration.
	ProviderConfigHub ProviderType = "ConfigHub"
	// ProviderOCI publishes Unit data verbatim in its Space's Releases, which a puller
	// (e.g. Argo CD or Flux) consumes from ConfigHub's OCI registry.
	ProviderOCI ProviderType = "OCI"
	// ProviderNone is used to express the Unit is not in use in a Release
	// or to be applied
	ProviderNone ProviderType = "None"
)

var SupportedProviders = map[ProviderType]bool{
	ProviderConfigHub: true,
	ProviderOCI:       true,
	ProviderNone:      true,
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
