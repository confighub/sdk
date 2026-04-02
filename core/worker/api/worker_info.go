// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package api

type WorkerInfo struct {
	IsServerWorker     bool               `json:",omitempty" description:"If true, this is a server-hosted worker."`
	UseUserIdentity    bool               `json:",omitempty" description:"If true, the server worker operates using the requesting user's identity rather than the worker's bot identity. Requires IsServerWorker to be true."`
	BridgeWorkerInfo   BridgeWorkerInfo   `description:"BridgeWorker capabilities"`
	FunctionWorkerInfo FunctionWorkerInfo `description:"FunctionWorker capabilities"`
}
