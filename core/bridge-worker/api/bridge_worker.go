// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package api

import (
	"context"

	"github.com/confighub/sdk/workerapi"
)

type InfoOptions struct {
	WorkerSlug string
}

// BridgeWorkerID identifies a bridge implementation by its ProviderType and
// the ToolchainTypes it supports. Each bridge returns this from ID() so that
// the worker can register it with the dispatcher without external mapping.
type BridgeWorkerID struct {
	ProviderType   ProviderType
	ToolchainTypes []workerapi.ToolchainType
}

type BridgeWorker interface {
	ID() BridgeWorkerID
	Info(InfoOptions) BridgeWorkerInfo
	Apply(BridgeWorkerContext, BridgeWorkerPayload) error
	Refresh(BridgeWorkerContext, BridgeWorkerPayload) error
	Import(BridgeWorkerContext, BridgeWorkerPayload) error
	Destroy(BridgeWorkerContext, BridgeWorkerPayload) error
	Finalize(BridgeWorkerContext, BridgeWorkerPayload) error
}

type WatchableWorker interface {
	WatchForApply(BridgeWorkerContext, BridgeWorkerPayload) error
	WatchForDestroy(BridgeWorkerContext, BridgeWorkerPayload) error
}

type BridgeWorkerContext interface {
	Context() context.Context
	GetServerURL() string
	GetWorkerID() string
	SendStatus(*ActionResult) error
}
