// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package workerapi

import "strings"

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

// AppConfigFileExtensions maps each AppConfig toolchain to the extension its files carry.
// It is the one place the correspondence lives: render-configmap names the ConfigMap key it
// renders a file into with it, and upload classifies a bundle's files by it, so a file that
// is rendered out and uploaded back is read as the format it was written as.
var AppConfigFileExtensions = map[ToolchainType]string{
	ToolchainAppConfigProperties: ".properties",
	ToolchainAppConfigYAML:       ".yaml",
	ToolchainAppConfigTOML:       ".toml",
	ToolchainAppConfigINI:        ".ini",
	ToolchainAppConfigJSON:       ".json",
	ToolchainAppConfigEnv:        ".env",
	ToolchainAppConfigText:       ".txt",
}

// FileExtensionForToolchain returns the file extension, with its leading dot, for a
// toolchain's files. A toolchain AppConfigFileExtensions does not list gets its format
// name, lowercased, and one with no format name gets ".config".
func FileExtensionForToolchain(toolchain ToolchainType) string {
	if ext, ok := AppConfigFileExtensions[toolchain]; ok {
		return ext
	}
	configFormat := strings.ToLower(strings.TrimPrefix(string(toolchain), "AppConfig/"))
	if configFormat == "" {
		return ".config"
	}
	return "." + configFormat
}
