// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package api

import "context"

type InfoOptions struct {
	Slug string
}

type BridgeWorker interface {
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
