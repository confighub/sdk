// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package workerapi

type ToolchainType string

// ToolchainType corresponds to the toolchain and configuration format+syntax
const (
	ToolchainConfigHubYAML       ToolchainType = "ConfigHub/YAML"
	ToolchainKubernetesYAML      ToolchainType = "Kubernetes/YAML"
	ToolchainAppConfigProperties ToolchainType = "AppConfig/Properties"
	ToolchainAppConfigYAML       ToolchainType = "AppConfig/YAML"
	ToolchainAppConfigTOML       ToolchainType = "AppConfig/TOML"
	ToolchainAppConfigINI        ToolchainType = "AppConfig/INI"
	ToolchainAppConfigJSON       ToolchainType = "AppConfig/JSON"
	ToolchainAppConfigEnv        ToolchainType = "AppConfig/Env"
	ToolchainAppConfigText       ToolchainType = "AppConfig/Text"

	// ToolchainAny is a wildcard toolchain used only on a Target (and the
	// SupportedConfigType a bridge advertises). A Target ConfigType with
	// ToolchainAny matches a Unit of any toolchain, which suits transports
	// that publish Unit data verbatim and never route by toolchain (e.g. OCI).
	// It must never appear as a Unit's toolchain: Units always carry a concrete
	// serialization format, which is what function execution and the published
	// artifact rely on. The value is the word "Any" (not "*") so it is slug-safe
	// and needs no shell quoting on the CLI.
	ToolchainAny ToolchainType = "Any"
)

const MaxToolchainTypeLength = 128

// AppConfigToolchains is the set of toolchains that carry application configuration
// files rather than infrastructure resources. Units of these toolchains hold config
// data that other Units consume, so they default to ProviderNone, but they may be
// given a Provider and released like any other Unit.
var AppConfigToolchains = map[ToolchainType]bool{
	ToolchainAppConfigProperties: true,
	ToolchainAppConfigYAML:       true,
	ToolchainAppConfigTOML:       true,
	ToolchainAppConfigINI:        true,
	ToolchainAppConfigJSON:       true,
	ToolchainAppConfigEnv:        true,
	ToolchainAppConfigText:       true,
}

// IsAppConfigToolchain reports whether the toolchain is an AppConfig format.
func IsAppConfigToolchain(toolchain ToolchainType) bool {
	return AppConfigToolchains[toolchain]
}
