// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package impl

import (
	"context"
	"fmt"
	"sync"

	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/confighub/sdk/bridge-worker/api"
	"github.com/confighub/sdk/workerapi"
)

// WorkerKey represents a unique identifier for a registered bridge worker
type WorkerKey struct {
	ToolchainType workerapi.ToolchainType
	ProviderType  api.ProviderType
}

// BridgeDispatcher is a bridge worker that delegates operations to registered workers
// based on the toolchain and provider information in the request payload
// It ensures operations on the same unit are processed sequentially
type BridgeDispatcher struct {
	mu              sync.RWMutex
	workers         map[WorkerKey]api.BridgeWorker
	ctx             context.Context
	cancel          context.CancelFunc
	disablePrefixes bool // Compatibility mode - disable target prefixes
}

// Ensure Dispatcher implements the BridgeWorker interface
var _ api.BridgeWorker = (*BridgeDispatcher)(nil)

// NewBridgeDispatcher creates a new Dispatcher instance with unit queue management
func NewBridgeDispatcher() *BridgeDispatcher {
	ctx, cancel := context.WithCancel(context.Background())

	d := &BridgeDispatcher{
		workers: make(map[WorkerKey]api.BridgeWorker),
		ctx:     ctx,
		cancel:  cancel,
	}

	return d
}

// SetDisablePrefixes configures the dispatcher to disable target prefixes (compatibility mode)
func (d *BridgeDispatcher) SetDisablePrefixes(disable bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.disablePrefixes = disable
}

// RegisterWorker registers a bridge worker for a specific toolchain and provider combination
func (d *BridgeDispatcher) RegisterWorker(toolchainType workerapi.ToolchainType, providerType api.ProviderType, worker api.BridgeWorker) {
	d.mu.Lock()
	defer d.mu.Unlock()

	key := WorkerKey{ToolchainType: toolchainType, ProviderType: providerType}
	d.workers[key] = worker
	log.Log.Info("Registered worker", "toolchainType", toolchainType, "providerType", providerType)
}

// GetWorker returns the appropriate worker for the given toolchain and provider types
func (d *BridgeDispatcher) getWorker(toolchainType workerapi.ToolchainType, providerType api.ProviderType) (api.BridgeWorker, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	key := WorkerKey{ToolchainType: toolchainType, ProviderType: providerType}
	worker, ok := d.workers[key]
	if !ok {
		return nil, fmt.Errorf("no worker registered for toolchain type '%s' and provider type '%s'", toolchainType, providerType)
	}

	return worker, nil
}

// getProviderPrefix returns the appropriate prefix for a given provider type
func (d *BridgeDispatcher) getProviderPrefix(providerType api.ProviderType) string {
	switch providerType {
	case api.ProviderKubernetes:
		return "k8s-"
	case api.ProviderFluxOCIWriter:
		return "flux-"
	case api.ProviderConfigMap:
		return "cm-"
	case api.ProviderAWS:
		return "aws-"
	default:
		return ""
	}
}

// Info returns aggregated information about all registered workers
func (d *BridgeDispatcher) Info(opts api.InfoOptions) api.BridgeWorkerInfo {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var supportedConfigTypes []*api.ConfigType

	// Collect info from all registered workers
	for key, worker := range d.workers {
		// TODO [ck]: Bridge should support TargetPrefix option for exposing unique targets
		prefix := ""
		if !d.disablePrefixes {
			prefix = d.getProviderPrefix(key.ProviderType)
		}
		opt := api.InfoOptions{
			Slug: prefix + opts.Slug,
		}
		info := worker.Info(opt)
		for _, configType := range info.SupportedConfigTypes {
			supportedConfigTypes = append(supportedConfigTypes, configType)
		}
	}

	return api.BridgeWorkerInfo{
		SupportedConfigTypes: supportedConfigTypes,
	}
}

// Apply delegates the Apply operation to the appropriate worker
// and ensures operations on the same unit ID are processed sequentially
func (d *BridgeDispatcher) Apply(ctx api.BridgeWorkerContext, payload api.BridgeWorkerPayload) error {
	worker, err := d.getWorker(payload.ToolchainType, payload.ProviderType)
	if err != nil {
		return err
	}

	log.Log.Info("Executing Apply operation with unit-level serialization",
		"toolchainType", payload.ToolchainType,
		"providerType", payload.ProviderType,
		"unitSlug", payload.UnitSlug,
		"unitID", payload.UnitID)

	return worker.Apply(ctx, payload)
}

// Refresh delegates the Refresh operation to the appropriate worker
// and ensures operations on the same unit ID are processed sequentially
func (d *BridgeDispatcher) Refresh(ctx api.BridgeWorkerContext, payload api.BridgeWorkerPayload) error {
	worker, err := d.getWorker(payload.ToolchainType, payload.ProviderType)
	if err != nil {
		return err
	}

	log.Log.Info("Executing Refresh operation with unit-level serialization",
		"toolchainType", payload.ToolchainType,
		"providerType", payload.ProviderType,
		"unitSlug", payload.UnitSlug,
		"unitID", payload.UnitID)

	return worker.Refresh(ctx, payload)
}

// Import delegates the Import operation to the appropriate worker
// and ensures operations on the same unit ID are processed sequentially
func (d *BridgeDispatcher) Import(ctx api.BridgeWorkerContext, payload api.BridgeWorkerPayload) error {
	worker, err := d.getWorker(payload.ToolchainType, payload.ProviderType)
	if err != nil {
		return err
	}

	log.Log.Info("Executing Import operation with unit-level serialization",
		"toolchainType", payload.ToolchainType,
		"providerType", payload.ProviderType,
		"unitSlug", payload.UnitSlug,
		"unitID", payload.UnitID)

	return worker.Import(ctx, payload)
}

// Destroy delegates the Destroy operation to the appropriate worker
// and ensures operations on the same unit ID are processed sequentially
func (d *BridgeDispatcher) Destroy(ctx api.BridgeWorkerContext, payload api.BridgeWorkerPayload) error {
	worker, err := d.getWorker(payload.ToolchainType, payload.ProviderType)
	if err != nil {
		return err
	}

	log.Log.Info("Executing Destroy operation with unit-level serialization",
		"toolchainType", payload.ToolchainType,
		"providerType", payload.ProviderType,
		"unitSlug", payload.UnitSlug,
		"unitID", payload.UnitID)

	return worker.Destroy(ctx, payload)
}

// Finalize delegates the Finalize operation to the appropriate worker
// and ensures operations on the same unit ID are processed sequentially
func (d *BridgeDispatcher) Finalize(ctx api.BridgeWorkerContext, payload api.BridgeWorkerPayload) error {
	worker, err := d.getWorker(payload.ToolchainType, payload.ProviderType)
	if err != nil {
		return err
	}

	log.Log.Info("Executing Finalize operation with unit-level serialization",
		"toolchainType", payload.ToolchainType,
		"providerType", payload.ProviderType,
		"unitSlug", payload.UnitSlug,
		"unitID", payload.UnitID)

	return worker.Finalize(ctx, payload)
}

func (d *BridgeDispatcher) WatchForApply(wctx api.BridgeWorkerContext, payload api.BridgeWorkerPayload) error {
	worker, err := d.getWorker(payload.ToolchainType, payload.ProviderType)
	if err != nil {
		return err
	}

	if watchable, ok := worker.(api.WatchableWorker); ok {
		return watchable.WatchForApply(wctx, payload)
	}

	return nil
}

func (d *BridgeDispatcher) WatchForDestroy(wctx api.BridgeWorkerContext, payload api.BridgeWorkerPayload) error {
	worker, err := d.getWorker(payload.ToolchainType, payload.ProviderType)
	if err != nil {
		return err
	}

	if watchable, ok := worker.(api.WatchableWorker); ok {
		return watchable.WatchForDestroy(wctx, payload)
	}

	return nil
}
