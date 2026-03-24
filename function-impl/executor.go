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
	"github.com/confighub/sdk/core/configkit/cubkit"
	"github.com/confighub/sdk/configkit/envkit"
	"github.com/confighub/sdk/configkit/hclkit"
	"github.com/confighub/sdk/configkit/inikit"
	"github.com/confighub/sdk/configkit/jsonkit"
	"github.com/confighub/sdk/configkit/k8skit"
	"github.com/confighub/sdk/configkit/propkit"
	"github.com/confighub/sdk/configkit/textkit"
	"github.com/confighub/sdk/configkit/tomlkit"
	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/function/executor"
	"github.com/confighub/sdk/core/function/handler"
	"github.com/confighub/sdk/function-impl/appjson"
	"github.com/confighub/sdk/function-impl/appyaml"
	"github.com/confighub/sdk/function-impl/confighub"
	"github.com/confighub/sdk/function-impl/env"
	"github.com/confighub/sdk/function-impl/generic"
	"github.com/confighub/sdk/function-impl/ini"
	"github.com/confighub/sdk/function-impl/kubernetes"
	"github.com/confighub/sdk/function-impl/opentofu"
	"github.com/confighub/sdk/function-impl/properties"
	"github.com/confighub/sdk/function-impl/text"
	"github.com/confighub/sdk/function-impl/toml"
	"github.com/confighub/sdk/core/workerapi"
)

// toolchainSetup pairs a provider with its function registration function.
type toolchainSetup struct {
	provider   handler.ToolchainProvider
	registerFn func(handler.FunctionRegistry)
}

// NewStandardExecutor creates a new FunctionExecutor with the standard functions registered.
// If toolchainTypes is non-nil, only toolchains in the list are registered.
// If toolchainTypes is nil, all toolchains are registered.
func NewStandardExecutor(toolchainTypes []workerapi.ToolchainType) *executor.ConcreteFunctionExecutor {
	return NewStandardExecutorWithAttributes(toolchainTypes, nil)
}

// NewStandardExecutorWithAttributes creates a new FunctionExecutor with the standard functions
// registered, plus any dynamic attributes. Attributes are registered directly on the
// FunctionHandler before signatures are captured, avoiding the map-copy issue.
// If toolchainTypes is non-nil, only toolchains in the list are registered.
// If toolchainTypes is nil, all toolchains are registered.
func NewStandardExecutorWithAttributes(toolchainTypes []workerapi.ToolchainType, attributes []executor.AttributeRegistration) *executor.ConcreteFunctionExecutor {
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
	hclRP := hclkit.NewHclResourceProvider()
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
		{hclRP, func(fh handler.FunctionRegistry) { opentofu.RegisterFunctions(hclRP, fh) }},
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

		fh := exec.CreateAndRegisterHandler(setup.provider)
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
				// Use user-provided invocations if present, otherwise default to get-/set-<name>
				getterInvocation := entry.GetterInvocation
				if getterInvocation == nil && !isBuiltIn {
					getterInvocation = &api.FunctionInvocation{
						FunctionName: "get-" + name,
					}
				}
				setterInvocation := entry.SetterInvocation
				if setterInvocation == nil && !isBuiltIn {
					setterInvocation = &api.FunctionInvocation{
						FunctionName: "set-" + name,
					}
				}

				yamlkit.RegisterPathsByAttributeName(
					resourceProvider,
					attr.AttributeName,
					entry.ResourceType,
					entry.Paths,
					getterInvocation,
					setterInvocation,
					false,
				)
			}

			// Register getter/setter functions only for non-built-in attributes
			if !isBuiltIn {
				// Detect $visitor setter pattern in path Details to enable defaults mode
				defaults := false
			outer:
				for _, entry := range attr.ResourceTypePaths {
					for _, pathInfo := range entry.Paths {
						if pathInfo.Details != nil {
							for _, si := range pathInfo.Details.SetterInvocations {
								if si.FunctionName == yamlkit.VisitorSetterInvocationFunctionName {
									defaults = true
									break outer
								}
							}
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
