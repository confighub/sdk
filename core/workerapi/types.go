// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package workerapi

type ToolchainType string

// ToolchainType corresponds to the toolchain and configuration format+syntax
const (
	ToolchainConfigHubYAML       ToolchainType = "ConfigHub/YAML"
	ToolchainKubernetesYAML      ToolchainType = "Kubernetes/YAML"
	ToolchainOpenTofuHCL         ToolchainType = "OpenTofu/HCL"
	ToolchainAppConfigProperties ToolchainType = "AppConfig/Properties"
	ToolchainAppConfigYAML       ToolchainType = "AppConfig/YAML"
	ToolchainAppConfigTOML       ToolchainType = "AppConfig/TOML"
	ToolchainAppConfigINI        ToolchainType = "AppConfig/INI"
	ToolchainAppConfigJSON       ToolchainType = "AppConfig/JSON"
	ToolchainAppConfigEnv        ToolchainType = "AppConfig/Env"
	ToolchainAppConfigText       ToolchainType = "AppConfig/Text"
)

const MaxToolchainTypeLength = 128
