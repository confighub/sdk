// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

// Package functions is the main entry point for the function executor.
// It is responsible for registering functions and invoking them.

// The executor currently supports the following toolchains:
// - ConfigHub YAML
// - Kubernetes YAML
// - OpenTofu HCL
// - AppConfig Properties
// - AppConfig YAML
// - AppConfig TOML
// - AppConfig INI

// Example:
//
//	func main() {
//		executor := NewEmptyExecutor()
//		executor.RegisterFunction(workerapi.ToolchainKubernetesYAML, handler.FunctionRegistration{
//			FunctionSignature: api.FunctionSignature{
//				FunctionName: "hello-world",
//				FunctionType: api.FunctionTypeCustom,
//			},
//		})
//	}

// Once the executor is initialized, it can be used to invoke functions. The FunctionExecutor can
// be used in conjunction with worker.ConfighubConnector to receive function invocations from ConfigHub.
package impl

import (
	"context"
	"fmt"

	"github.com/confighub/sdk/configkit/yamlkit"
	"github.com/confighub/sdk/function/api"
	"github.com/confighub/sdk/function/handler"
	"github.com/confighub/sdk/function-impl/generic"
	"github.com/confighub/sdk/workerapi"
)

// implementation notes:
// The executor (along with worker.ConfighubConnector) is currently a wrapper around other code in the public repo for the purpose of exploring
// a clear and simple public API for custom function authoring and execution.
// Some experimentation with the interface is expected and as well as some refactoring behind the scenes.

type FunctionExecutor struct {
	signatureRegistry map[workerapi.ToolchainType]map[string]api.FunctionSignature
	functionRegistry  map[workerapi.ToolchainType]handler.FunctionHandler
	resourceProviders map[workerapi.ToolchainType]yamlkit.ResourceProvider
}

// NewEmptyExecutor creates a new FunctionExecutor with no functions registered.
func NewEmptyExecutor() *FunctionExecutor {
	return &FunctionExecutor{
		signatureRegistry: make(map[workerapi.ToolchainType]map[string]api.FunctionSignature),
		functionRegistry:  make(map[workerapi.ToolchainType]handler.FunctionHandler),
		resourceProviders: make(map[workerapi.ToolchainType]yamlkit.ResourceProvider),
	}
}

// NewStandardExecutor creates a new FunctionExecutor with the standard functions registered.
// This is a convenience function that creates a new executor with the standard functions registered
// for all toolchains.
func NewStandardExecutor() *FunctionExecutor {
	executor := NewEmptyExecutor()

	registrators := map[workerapi.ToolchainType]func(*handler.FunctionHandler){
		workerapi.ToolchainConfigHubYAML:       RegisterConfigHub,
		workerapi.ToolchainKubernetesYAML:      RegisterKubernetes,
		workerapi.ToolchainOpenTofuHCL:         RegisterOpenTofu,
		workerapi.ToolchainAppConfigProperties: RegisterProperties,
		workerapi.ToolchainAppConfigYAML:       RegisterAppConfigYAML,
		workerapi.ToolchainAppConfigTOML:       RegisterTOML,
		workerapi.ToolchainAppConfigINI:        RegisterINI,
	}

	for toolchain, registrator := range registrators {
		fh := handler.NewFunctionHandler()
		registrator(fh)
		executor.functionRegistry[toolchain] = *fh
		executor.resourceProviders[toolchain] = fh.GetResourceProvider()
		executor.signatureRegistry[toolchain] = make(map[string]api.FunctionSignature)
		for name, registration := range fh.ListCore() {
			executor.signatureRegistry[toolchain][name] = registration.FunctionSignature
		}
	}
	return executor
}

func (e *FunctionExecutor) RegisterFunction(toolchain workerapi.ToolchainType, registration handler.FunctionRegistration) error {
	if _, ok := e.signatureRegistry[toolchain]; !ok {
		// if this is the first time we're registering a function for this toolchain,
		// we need to initialize the signature registry for this toolchain
		e.signatureRegistry[toolchain] = make(map[string]api.FunctionSignature)
	}
	if _, ok := e.signatureRegistry[toolchain][registration.FunctionSignature.FunctionName]; ok {
		return fmt.Errorf("function %s already registered", registration.FunctionSignature.FunctionName)
	}
	e.signatureRegistry[toolchain][registration.FunctionSignature.FunctionName] = registration.FunctionSignature

	functionHandler, ok := e.functionRegistry[toolchain]
	if !ok {
		// if this is the first time we're registering a function for this toolchain,
		// we need to initialize the function handler for this toolchain
		newHandler := handler.NewFunctionHandler()

		// Registrators set converter and resource provider on the handler.
		registrators := map[workerapi.ToolchainType]func(*handler.FunctionHandler){
			workerapi.ToolchainConfigHubYAML:       RegisterConfigHub,
			workerapi.ToolchainKubernetesYAML:      RegisterKubernetes,
			workerapi.ToolchainOpenTofuHCL:         RegisterOpenTofu,
			workerapi.ToolchainAppConfigProperties: RegisterProperties,
			workerapi.ToolchainAppConfigYAML:       RegisterAppConfigYAML,
			workerapi.ToolchainAppConfigTOML:       RegisterTOML,
			workerapi.ToolchainAppConfigINI:        RegisterINI,
		}
		registrator, hasRegistrator := registrators[toolchain]
		if !hasRegistrator {
			return fmt.Errorf("no registrator found for toolchain %s", toolchain)
		}
		registrator(newHandler)

		// compute-mutations is a required standard function that will be used during execution of
		// any function registered with this handler. Therefore we need to register it here.
		generic.RegisterComputeMutations(newHandler, newHandler.GetConverter(), newHandler.GetResourceProvider())
		e.functionRegistry[toolchain] = *newHandler
		e.resourceProviders[toolchain] = newHandler.GetResourceProvider()
		functionHandler = *newHandler
	}
	functionHandler.RegisterFunction(registration.FunctionSignature.FunctionName, &registration)

	return nil
}

// GetResourceProvider returns the ResourceProvider for the given toolchain type.
func (e *FunctionExecutor) GetResourceProvider(toolchain workerapi.ToolchainType) (yamlkit.ResourceProvider, bool) {
	rp, ok := e.resourceProviders[toolchain]
	return rp, ok
}

// ResourceTypePathsEntry pairs a resource type with paths and optional getter/setter invocations.
// Each entry corresponds to one call to yamlkit.RegisterPathsByAttributeName.
// For non-built-in attributes, invocations default to get-<slug>/set-<slug> if nil.
type ResourceTypePathsEntry struct {
	ResourceType     api.ResourceType
	Paths            api.PathToVisitorInfoType
	GetterInvocation *api.FunctionInvocation
	SetterInvocation *api.FunctionInvocation
}

// AttributeRegistration contains the information needed to register a dynamic attribute.
// The AttributeName is used as the slug for generating getter/setter function names
// (get-<name>, set-<name>).
type AttributeRegistration struct {
	AttributeName     api.AttributeName
	ToolchainType     workerapi.ToolchainType
	Description       string
	ResourceTypePaths []ResourceTypePathsEntry
	Parameters        []api.FunctionParameter
}

// NewStandardExecutorWithAttributes creates a new FunctionExecutor with the standard functions
// registered, plus any dynamic attributes. Attributes are registered directly on the
// FunctionHandler before it is stored, avoiding the map-copy issue.
func NewStandardExecutorWithAttributes(attributes []AttributeRegistration) *FunctionExecutor {
	executor := NewEmptyExecutor()

	registrators := map[workerapi.ToolchainType]func(*handler.FunctionHandler){
		workerapi.ToolchainConfigHubYAML:       RegisterConfigHub,
		workerapi.ToolchainKubernetesYAML:      RegisterKubernetes,
		workerapi.ToolchainOpenTofuHCL:         RegisterOpenTofu,
		workerapi.ToolchainAppConfigProperties: RegisterProperties,
		workerapi.ToolchainAppConfigYAML:       RegisterAppConfigYAML,
		workerapi.ToolchainAppConfigTOML:       RegisterTOML,
		workerapi.ToolchainAppConfigINI:        RegisterINI,
	}

	for toolchain, registrator := range registrators {
		fh := handler.NewFunctionHandler()
		registrator(fh)

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

		executor.functionRegistry[toolchain] = *fh
		executor.resourceProviders[toolchain] = fh.GetResourceProvider()
		executor.signatureRegistry[toolchain] = make(map[string]api.FunctionSignature)
		for name, registration := range fh.ListCore() {
			executor.signatureRegistry[toolchain][name] = registration.FunctionSignature
		}
	}
	return executor
}

func (e *FunctionExecutor) RegisteredFunctions() map[workerapi.ToolchainType]map[string]api.FunctionSignature {
	return e.signatureRegistry
}

func (e *FunctionExecutor) Invoke(ctx context.Context, functionInvocation *api.FunctionInvocationRequest) (*api.FunctionInvocationResponse, error) {
	handler, ok := e.functionRegistry[functionInvocation.ToolchainType]
	if !ok {
		return nil, fmt.Errorf("no handler found for toolchain %s", functionInvocation.ToolchainType)
	}
	return handler.InvokeCore(ctx, functionInvocation)
}
