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
	ToolchainAppConfigTOML       ToolchainType = "AppConfig/TOML" // TODO
	ToolchainAppConfigINI        ToolchainType = "AppConfig/INI"  // TODO
	ToolchainAppConfigEnv        ToolchainType = "AppConfig/Env"  // TODO
)

const MaxToolchainTypeLength = 128
