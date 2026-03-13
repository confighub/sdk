// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package api

type WorkerInfo struct {
	IsServerWorker     bool               `json:",omitempty" description:"If true, this is the server-hosted worker. Only one per organization."`
	BridgeWorkerInfo   BridgeWorkerInfo   `description:"BridgeWorker capabilities"`
	FunctionWorkerInfo FunctionWorkerInfo `description:"FunctionWorker capabilities"`
}
