// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package worker

import (
	"github.com/confighub/sdk/worker/api"
	"github.com/confighub/sdk/worker/lib"
)

// NewBridgeDispatcher creates a new worker.BridgeDispatcher (different from lib.BridgeDispatcher)
// which is currently only used in the connector. The reason is to experiment with a simplifying
// wrapper layer over the existing implementation.
func NewBridgeDispatcher() BridgeDispatcher {
	wbd := BridgeDispatcher{
		bridgeDispatcher: lib.NewBridgeDispatcher(),
	}
	return wbd
}

type BridgeDispatcher struct {
	bridgeDispatcher *lib.BridgeDispatcher
}

// RegisterBridge registers a bridge with the dispatcher. It is simpler and more clear than the
// existing RegisterWorker method on lib.BridgeDispatcher.
func (b *BridgeDispatcher) RegisterBridge(bridge api.Bridge) {
	configTypes := bridge.Info(api.InfoOptions{})
	for _, configType := range configTypes.SupportedConfigTypes {
		b.bridgeDispatcher.RegisterWorker(configType.ToolchainType, configType.ProviderType, bridge)
	}
}

// getWrapped returns the underlying BridgeDispatcher. It is only used internally in the package
// so we could just access the underlying BridgeDispatcher directly. But this creates more clarity.
func (b *BridgeDispatcher) getWrapped() *lib.BridgeDispatcher {
	return b.bridgeDispatcher
}
