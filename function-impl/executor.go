// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

// Package impl is the main entry point for the function executor.
// It provides NewStandardExecutor and NewStandardExecutorWithAttributes for
// creating executors with all built-in toolchain functions registered.
//
// For custom function executors that don't need all built-in functions,
// use executor.NewEmptyExecutor() from the core executor package.
package impl

import (
	"github.com/confighub/sdk/configkit/appyamlkit"
	"github.com/confighub/sdk/configkit/envkit"
	"github.com/confighub/sdk/configkit/inikit"
	"github.com/confighub/sdk/configkit/jsonkit"
	"github.com/confighub/sdk/configkit/k8skit"
	"github.com/confighub/sdk/configkit/propkit"
	"github.com/confighub/sdk/configkit/textkit"
	"github.com/confighub/sdk/configkit/tomlkit"
	"github.com/confighub/sdk/core/configkit/cubkit"
	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/function/executor"
	"github.com/confighub/sdk/core/function/handler"
	"github.com/confighub/sdk/core/workerapi"
	"github.com/confighub/sdk/function-impl/appjson"
	"github.com/confighub/sdk/function-impl/appyaml"
	"github.com/confighub/sdk/function-impl/confighub"
	"github.com/confighub/sdk/function-impl/env"
	"github.com/confighub/sdk/function-impl/generic"
	"github.com/confighub/sdk/function-impl/ini"
	"github.com/confighub/sdk/function-impl/kubernetes"
	"github.com/confighub/sdk/function-impl/properties"
	"github.com/confighub/sdk/function-impl/text"
	"github.com/confighub/sdk/function-impl/toml"
)

// toolchainSetup pairs a provider with its function registration function.
type toolchainSetup struct {
	provider   handler.ToolchainProvider
	registerFn func(handler.FunctionRegistry)
}

// NewStandardExecutor creates a new FunctionExecutor with the standard functions registered.
// If toolchainTypes is non-nil, only toolchains in the list are registered.
// If toolchainTypes is nil, all toolchains are registered.
func NewStandardExecutor(toolchainTypes []workerapi.ToolchainType, registerFunctions bool) *executor.ConcreteFunctionExecutor {
	return NewStandardExecutorWithAttributes(toolchainTypes, registerFunctions, nil)
}

// NewStandardExecutorWithAttributes creates a new FunctionExecutor with the standard functions
// registered, plus any dynamic attributes. Attributes are registered directly on the
// FunctionHandler before signatures are captured, avoiding the map-copy issue.
// If toolchainTypes is non-nil, only toolchains in the list are registered.
// If toolchainTypes is nil, all toolchains are registered.
func NewStandardExecutorWithAttributes(toolchainTypes []workerapi.ToolchainType, registerFunctions bool, attributes []executor.AttributeRegistration) *executor.ConcreteFunctionExecutor {
	exec := executor.NewEmptyExecutor()

	// Build a set for fast lookup when filtering is requested
	var toolchainSet map[workerapi.ToolchainType]bool
	if toolchainTypes != nil {
		toolchainSet = make(map[workerapi.ToolchainType]bool, len(toolchainTypes))
		for _, tt := range toolchainTypes {
			toolchainSet[tt] = true
		}
	}

	cubRP := cubkit.NewConfigHubResourceProvider()
	k8sRP := k8skit.NewK8sResourceProvider()
	propRP := propkit.NewPropertiesResourceProvider()
	appyamlRP := appyamlkit.NewAppConfigYAMLResourceProvider()
	tomlRP := tomlkit.NewTOMLResourceProvider()
	iniRP := inikit.NewINIResourceProvider()
	jsonRP := jsonkit.NewJSONResourceProvider()
	envRP := envkit.NewEnvResourceProvider()
	textRP := textkit.NewTextResourceProvider()

	setups := []toolchainSetup{
		{cubRP, func(fh handler.FunctionRegistry) { confighub.RegisterFunctions(cubRP, fh) }},
		{k8sRP, func(fh handler.FunctionRegistry) { kubernetes.RegisterFunctions(k8sRP, fh) }},
		{propRP, func(fh handler.FunctionRegistry) { properties.RegisterFunctions(propRP, fh) }},
		{appyamlRP, func(fh handler.FunctionRegistry) { appyaml.RegisterFunctions(appyamlRP, fh) }},
		{tomlRP, func(fh handler.FunctionRegistry) { toml.RegisterFunctions(tomlRP, fh) }},
		{iniRP, func(fh handler.FunctionRegistry) { ini.RegisterFunctions(iniRP, fh) }},
		{jsonRP, func(fh handler.FunctionRegistry) { appjson.RegisterFunctions(jsonRP, fh) }},
		{envRP, func(fh handler.FunctionRegistry) { env.RegisterFunctions(envRP, fh) }},
		{textRP, func(fh handler.FunctionRegistry) { text.RegisterFunctions(textRP, fh) }},
	}

	for _, setup := range setups {
		toolchain := setup.provider.GetToolchainType()

		// Skip toolchains not in the requested list
		if toolchainSet != nil && !toolchainSet[toolchain] {
			continue
		}

		if !registerFunctions {
			exec.RegisterToolchain(setup.provider, true)
			continue
		}

		fh := exec.RegisterToolchain(setup.provider, false)
		setup.registerFn(fh)

		// Register dynamic attributes for this toolchain
		for _, attr := range attributes {
			if attr.ToolchainType != toolchain {
				continue
			}
			resourceProvider := fh.GetResourceProvider()
			name := string(attr.AttributeName)

			// Check if this attribute name is already registered by the standard registrator
			_, isBuiltIn := fh.GetPathRegistry()[attr.AttributeName]

			// Register paths for each ResourceType entry
			for _, entry := range attr.ResourceTypePaths {
				yamlkit.RegisterPathsByAttributeName(
					resourceProvider,
					attr.AttributeName,
					entry.ResourceType,
					entry.Paths,
					&yamlkit.AttributeRegistrationDetails{
						AttributeNeedsProvidesDetails: entry.AttributeNeedsProvidesDetails,
					},
					false, false,
				)
			}

			// Register getter/setter functions only for non-built-in attributes
			if !isBuiltIn {
				// A path carrying a default value puts the attribute in defaults mode: its
				// setter takes no value, writing what each path declares instead.
				defaults := false
			outer:
				for _, entry := range attr.ResourceTypePaths {
					for _, pathInfo := range entry.Paths {
						if pathInfo.Details != nil && pathInfo.Details.DefaultValue != nil {
							defaults = true
							break outer
						}
					}
				}
				generic.RegisterPathSetterAndGetter(
					fh,
					name,
					attr.Parameters,
					attr.Description,
					attr.AttributeName,
					resourceProvider,
					true,     // addSetter
					defaults, // upsert
					defaults, // defaults
				)
			}
		}

		exec.CaptureSignatures(toolchain)
	}
	return exec
}
