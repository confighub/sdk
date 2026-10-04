// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package api

type WorkerInfo struct {
	// IsServerWorker says that no worker process is expected to connect: the worker is an
	// identity only, and runs no functions. It is fixed when the worker is created.
	IsServerWorker     bool               `json:",omitempty" immutable:"true" description:"If true, this is a server-hosted worker: an identity that no worker process connects as, and that runs no functions. It cannot be changed after the worker is created."`
	FunctionWorkerInfo FunctionWorkerInfo `description:"FunctionWorker capabilities"`
}
