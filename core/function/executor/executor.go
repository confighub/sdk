// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

// Package executor defines the FunctionExecutor interface and provides a concrete
// implementation that manages function handlers per toolchain type.
package executor

import (
	"context"
	"fmt"

	bwapi "github.com/confighub/sdk/worker/api"
	"github.com/confighub/sdk/configkit"
	"github.com/confighub/sdk/configkit/yamlkit"
	funcapi "github.com/confighub/sdk/function/api"
	"github.com/confighub/sdk/function/handler"
	"github.com/confighub/sdk/workerapi"
)

// FunctionExecutor is the interface that function executors must implement to be
// used with the worker connector. This avoids importing the concrete function
// package (which pulls in all built-in handler implementations and their dependencies).
type FunctionExecutor interface {
	RegisteredFunctions() map[workerapi.ToolchainType]map[string]funcapi.FunctionSignature
	Invoke(ctx context.Context, functionInvocation *funcapi.FunctionInvocationRequest) (*funcapi.FunctionInvocationResponse, error)
	Info() bwapi.FunctionWorkerInfo
}

// ConcreteFunctionExecutor is the concrete implementation of FunctionExecutor.
// It manages function handlers per toolchain type and supports registering
// toolchains and individual functions.
type ConcreteFunctionExecutor struct {
	signatureRegistry map[workerapi.ToolchainType]map[string]funcapi.FunctionSignature
	functionRegistry  map[workerapi.ToolchainType]*handler.FunctionHandler
}

// Ensure ConcreteFunctionExecutor implements FunctionExecutor
var _ FunctionExecutor = (*ConcreteFunctionExecutor)(nil)

// NewEmptyExecutor creates a new ConcreteFunctionExecutor with no functions registered.
func NewEmptyExecutor() *ConcreteFunctionExecutor {
	return &ConcreteFunctionExecutor{
		signatureRegistry: make(map[workerapi.ToolchainType]map[string]funcapi.FunctionSignature),
		functionRegistry:  make(map[workerapi.ToolchainType]*handler.FunctionHandler),
	}
}

// RegisterToolchain registers a toolchain with its resource provider.
// The toolchain type is derived from the provider's GetToolchainType method.
// This creates a new FunctionHandler with the provider, which automatically
// registers the compute-mutations function.
func (e *ConcreteFunctionExecutor) RegisterToolchain(provider handler.ToolchainProvider) {
	toolchain := provider.GetToolchainType()
	fh := handler.NewFunctionHandler(provider)
	e.functionRegistry[toolchain] = fh
	if _, ok := e.signatureRegistry[toolchain]; !ok {
		e.signatureRegistry[toolchain] = make(map[string]funcapi.FunctionSignature)
	}
	// Capture any functions already registered (e.g., compute-mutations)
	for name, registration := range fh.ListCore() {
		e.signatureRegistry[toolchain][name] = registration.FunctionSignature
	}
}

// RegisterFunction registers a single function for a toolchain.
// If the registration has a FunctionInit, it is called before registration.
// Returns an error if the toolchain is not initialized or if FunctionInit fails.
func (e *ConcreteFunctionExecutor) RegisterFunction(toolchain workerapi.ToolchainType, registration handler.FunctionRegistration) error {
	if registration.FunctionInit != nil {
		if err := registration.FunctionInit(); err != nil {
			return fmt.Errorf("initialization failed for function %s: %w", registration.FunctionSignature.FunctionName, err)
		}
	}

	if _, ok := e.signatureRegistry[toolchain]; !ok {
		e.signatureRegistry[toolchain] = make(map[string]funcapi.FunctionSignature)
	}
	if _, ok := e.signatureRegistry[toolchain][registration.FunctionSignature.FunctionName]; ok {
		return fmt.Errorf("function %s already registered", registration.FunctionSignature.FunctionName)
	}
	e.signatureRegistry[toolchain][registration.FunctionSignature.FunctionName] = registration.FunctionSignature

	fh, ok := e.functionRegistry[toolchain]
	if !ok {
		return fmt.Errorf("toolchain %s not initialized; call RegisterToolchain first or use NewStandardExecutor", toolchain)
	}
	fh.RegisterFunction(registration.FunctionSignature.FunctionName, &registration)

	return nil
}

// RegisteredFunctions returns the signature registry.
func (e *ConcreteFunctionExecutor) RegisteredFunctions() map[workerapi.ToolchainType]map[string]funcapi.FunctionSignature {
	return e.signatureRegistry
}

// Info builds a FunctionWorkerInfo from the registered functions.
func (e *ConcreteFunctionExecutor) Info() bwapi.FunctionWorkerInfo {
	toolchainTypes := make([]workerapi.ToolchainType, 0, len(e.signatureRegistry))
	for tt := range e.signatureRegistry {
		toolchainTypes = append(toolchainTypes, tt)
	}
	return bwapi.FunctionWorkerInfo{
		ToolchainTypes:     toolchainTypes,
		SupportedFunctions: e.signatureRegistry,
	}
}

// Invoke routes a function invocation to the appropriate handler.
func (e *ConcreteFunctionExecutor) Invoke(ctx context.Context, functionInvocation *funcapi.FunctionInvocationRequest) (*funcapi.FunctionInvocationResponse, error) {
	fh, ok := e.functionRegistry[functionInvocation.ToolchainType]
	if !ok {
		return nil, fmt.Errorf("no handler found for toolchain %s", functionInvocation.ToolchainType)
	}
	return fh.InvokeCore(ctx, functionInvocation)
}

// GetResourceProvider returns the ResourceProvider for the given toolchain type.
func (e *ConcreteFunctionExecutor) GetResourceProvider(toolchain workerapi.ToolchainType) (yamlkit.ResourceProvider, bool) {
	fh, ok := e.functionRegistry[toolchain]
	if !ok {
		return nil, false
	}
	return fh.GetResourceProvider(), true
}

// GetConverter returns the ConfigConverter for the given toolchain type.
func (e *ConcreteFunctionExecutor) GetConverter(toolchain workerapi.ToolchainType) (configkit.ConfigConverter, bool) {
	fh, ok := e.functionRegistry[toolchain]
	if !ok {
		return nil, false
	}
	return fh.GetConverter(), true
}

// CreateAndRegisterHandler creates a new FunctionHandler for the given provider's toolchain
// and registers it. Call CaptureSignatures after populating the handler with functions.
func (e *ConcreteFunctionExecutor) CreateAndRegisterHandler(provider handler.ToolchainProvider) *handler.FunctionHandler {
	toolchain := provider.GetToolchainType()
	fh := handler.NewFunctionHandler(provider)
	e.functionRegistry[toolchain] = fh
	e.signatureRegistry[toolchain] = make(map[string]funcapi.FunctionSignature)
	return fh
}

// GetHandler returns the FunctionHandler for the given toolchain type.
func (e *ConcreteFunctionExecutor) GetHandler(toolchain workerapi.ToolchainType) (*handler.FunctionHandler, bool) {
	fh, ok := e.functionRegistry[toolchain]
	return fh, ok
}

// CaptureSignatures updates the signature registry from the handler's current registrations.
// Call after using CreateAndRegisterHandler to populate the handler via a registrar.
func (e *ConcreteFunctionExecutor) CaptureSignatures(toolchain workerapi.ToolchainType) {
	fh, ok := e.functionRegistry[toolchain]
	if !ok {
		return
	}
	if _, ok := e.signatureRegistry[toolchain]; !ok {
		e.signatureRegistry[toolchain] = make(map[string]funcapi.FunctionSignature)
	}
	for name, registration := range fh.ListCore() {
		e.signatureRegistry[toolchain][name] = registration.FunctionSignature
	}
}

// ResourceTypePathsEntry pairs a resource type with paths and optional getter/setter invocations.
type ResourceTypePathsEntry struct {
	ResourceType     funcapi.ResourceType
	Paths            funcapi.PathToVisitorInfoType
	GetterInvocation *funcapi.FunctionInvocation
	SetterInvocation *funcapi.FunctionInvocation
}

// AttributeRegistration contains the information needed to register a dynamic attribute.
type AttributeRegistration struct {
	AttributeName     funcapi.AttributeName
	ToolchainType     workerapi.ToolchainType
	Description       string
	ResourceTypePaths []ResourceTypePathsEntry
	Parameters        []funcapi.FunctionParameter
}
