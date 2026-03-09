// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package api

type WorkerInfo struct {
	BridgeWorkerInfo   BridgeWorkerInfo   `description:"BridgeWorker capabilities"`
	FunctionWorkerInfo FunctionWorkerInfo `description:"FunctionWorker capabilities"`
}
