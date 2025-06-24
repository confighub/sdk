// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package workerapi

type ToolchainType string

// ToolchainType corresponds to the toolchain and configuration format+syntax
const (
	ToolchainKubernetesYAML      ToolchainType = "Kubernetes/YAML"
	ToolchainOpenTofuHCL         ToolchainType = "OpenTofu/HCL"
	ToolchainAppConfigProperties ToolchainType = "AppConfig/Properties"
	ToolchainAppConfigTOML       ToolchainType = "AppConfig/TOML" // TODO
	ToolchainAppConfigINI        ToolchainType = "AppConfig/INI"  // TODO
	ToolchainAppConfigEnv        ToolchainType = "AppConfig/Env"  // TODO
)
