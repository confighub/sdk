// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package impl

import (
	"context"
	"fmt"
	"sync"

	"github.com/google/uuid"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/confighub/sdk/bridge-worker/api"
	funcApi "github.com/confighub/sdk/function/api"
	"github.com/confighub/sdk/workerapi"
)

// FunctionDispatcher is a function worker that delegates operations to registered workers
// based on the toolchain type information in the function request
// It ensures operations on the same unit are processed sequentially
type FunctionDispatcher struct {
	mu      sync.RWMutex
	workers map[workerapi.ToolchainType]api.FunctionWorker
	ctx     context.Context
	cancel  context.CancelFunc
}

// Ensure DispatcherFunctionWorker implements the FunctionWorker interface
var _ api.FunctionWorker = (*FunctionDispatcher)(nil)

// NewFunctionDispatcher creates a new DispatcherFunctionWorker instance
func NewFunctionDispatcher() *FunctionDispatcher {
	ctx, cancel := context.WithCancel(context.Background())

	d := &FunctionDispatcher{
		workers: make(map[workerapi.ToolchainType]api.FunctionWorker),
		ctx:     ctx,
		cancel:  cancel,
	}

	return d
}

// RegisterWorker registers a function worker for a specific toolchain type
func (d *FunctionDispatcher) RegisterWorker(toolchainType workerapi.ToolchainType, worker api.FunctionWorker) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.workers[toolchainType] = worker
	log.Log.Info("Registered function worker", "toolchainType", toolchainType)
}

// GetWorker returns the appropriate worker for the given toolchain type
func (d *FunctionDispatcher) getWorker(toolchainType workerapi.ToolchainType) (api.FunctionWorker, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	worker, ok := d.workers[toolchainType]
	if !ok {
		return nil, fmt.Errorf("no function worker registered for toolchain type '%s'", toolchainType)
	}

	return worker, nil
}

// Info returns aggregated information about all registered function workers
func (d *FunctionDispatcher) Info() api.FunctionWorkerInfo {
	d.mu.RLock()
	defer d.mu.RUnlock()

	supportedFunctions := make(map[workerapi.ToolchainType]map[string]funcApi.FunctionSignature)

	// Collect info from all registered workers
	for toolchainType, worker := range d.workers {
		info := worker.Info()
		if info.SupportedFunctions != nil {
			if _, exists := info.SupportedFunctions[toolchainType]; exists {
				if supportedFunctions[toolchainType] == nil {
					supportedFunctions[toolchainType] = make(map[string]funcApi.FunctionSignature)
				}

				// Add all supported functions from this worker for this toolchain type
				for functionName, signature := range info.SupportedFunctions[toolchainType] {
					supportedFunctions[toolchainType][functionName] = signature
				}
			}
		}
	}

	return api.FunctionWorkerInfo{
		SupportedFunctions: supportedFunctions,
	}
}

// extractUnitID extracts a unique identifier for the function invocation
// Uses the FunctionContext.UnitID if available, or generates a composite key using req details
func extractUnitID(req funcApi.FunctionInvocationRequest) string {
	// If function context has a UnitID, use it
	if req.UnitID != uuid.Nil {
		return req.UnitID.String()
	}

	// Otherwise create a composite key from SpaceID and UnitSlug
	if req.SpaceID != uuid.Nil && req.UnitSlug != "" {
		return fmt.Sprintf("%s:%s", req.SpaceID.String(), req.UnitSlug)
	}

	// Fallback to a default ID if nothing is available
	return "default-function-unit"
}

// Invoke delegates the function invocation to the appropriate worker
// and ensures operations on the same unit are processed sequentially
func (d *FunctionDispatcher) Invoke(ctx api.FunctionWorkerContext, req funcApi.FunctionInvocationRequest) (funcApi.FunctionInvocationResponse, error) {
	worker, err := d.getWorker(req.ToolchainType)
	if err != nil {
		return funcApi.FunctionInvocationResponse{}, err
	}

	// Extract a unit identifier to ensure serialization of operations on the same unit
	unitID := extractUnitID(req)

	log.Log.Info("Executing function invocation with unit-level serialization",
		"toolchainType", req.ToolchainType,
		"unitID", unitID,
		"functionNames", getFunctionNames(req))

	return worker.Invoke(ctx, req)
}

// getFunctionNames extracts the function names for logging purposes
func getFunctionNames(req funcApi.FunctionInvocationRequest) []string {
	names := make([]string, 0, len(req.FunctionInvocations))
	for _, invocation := range req.FunctionInvocations {
		names = append(names, invocation.FunctionName)
	}
	return names
}
