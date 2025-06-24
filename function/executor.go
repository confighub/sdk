// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

// Package functions is the main entry point for the function executor.
// It is responsible for registering functions and invoking them.

// The executor currently supports the following toolchains:
// - Kubernetes YAML
// - OpenTofu HCL
// - AppConfig Properties

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
package function

import (
	"context"
	"fmt"

	"github.com/confighub/sdk/configkit"
	"github.com/confighub/sdk/configkit/hclkit"
	"github.com/confighub/sdk/configkit/k8skit"
	"github.com/confighub/sdk/configkit/propkit"
	"github.com/confighub/sdk/function/api"
	"github.com/confighub/sdk/function/handler"
	"github.com/confighub/sdk/function/internal/handlers/generic"
	"github.com/confighub/sdk/workerapi"
)

// implementation notes:
// The executor (along with worker.ConfighubConnector) is currently a wrapper around other code in the public repo for the purpose of exploring
// a clear and simple public API for custom function authoring and execution.
// Some experimentation with the interface is expected and as well as some refactoring behind the scenes.

var converters = map[workerapi.ToolchainType]configkit.ConfigConverter{
	workerapi.ToolchainKubernetesYAML:      k8skit.K8sResourceProvider,
	workerapi.ToolchainOpenTofuHCL:         hclkit.HclResourceProvider,
	workerapi.ToolchainAppConfigProperties: propkit.PropertiesResourceProvider,
}

var registrators = map[workerapi.ToolchainType]func(*handler.FunctionHandler){
	workerapi.ToolchainKubernetesYAML:      RegisterKubernetes,
	workerapi.ToolchainOpenTofuHCL:         RegisterOpenTofu,
	workerapi.ToolchainAppConfigProperties: RegisterProperties,
}

type FunctionExecutor struct {
	signatureRegistry map[workerapi.ToolchainType]map[string]api.FunctionSignature
	functionRegistry  map[workerapi.ToolchainType]handler.FunctionHandler
}

// NewEmptyExecutor creates a new FunctionExecutor with no functions registered.
func NewEmptyExecutor() *FunctionExecutor {
	return &FunctionExecutor{
		signatureRegistry: make(map[workerapi.ToolchainType]map[string]api.FunctionSignature),
		functionRegistry:  make(map[workerapi.ToolchainType]handler.FunctionHandler),
	}
}

// NewStandardExecutor creates a new FunctionExecutor with the standard functions registered.
// This is a convenience function that creates a new executor with the standard functions registered
// for all toolchains.
func NewStandardExecutor() *FunctionExecutor {
	executor := NewEmptyExecutor()
	for toolchain, converter := range converters {
		// we could loop over registrators as well.
		handler := handler.NewFunctionHandler()
		handler.SetConverter(converter)
		registrators[toolchain](handler)
		executor.functionRegistry[toolchain] = *handler
		var signatureRegistry = make(map[string]api.FunctionSignature)
		for name, registration := range handler.ListCore() {
			signatureRegistry[name] = registration.FunctionSignature
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
		// function handler needs a converter to convert the input to the appropriate format specific to the toolchain
		converter, ok1 := converters[toolchain]
		if !ok1 {
			return fmt.Errorf("no converter found for toolchain %s", toolchain)
		}
		newHandler.SetConverter(converter)
		// compute-mutations is a required standard function that will be used during execution of
		// any function registered with this handler. Therefore we need to register it here.
		generic.RegisterComputeMutations(newHandler, converter, k8skit.K8sResourceProvider)
		e.functionRegistry[toolchain] = *newHandler
		functionHandler = *newHandler
	}
	functionHandler.RegisterFunction(registration.FunctionSignature.FunctionName, &registration)

	return nil
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
