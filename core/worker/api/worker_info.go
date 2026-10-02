// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package api

type WorkerInfo struct {
	// IsServerWorker and UseUserIdentity say where the worker runs and whose identity it acts
	// under. Both are fixed when the worker is created: changing either would hand an existing
	// worker's Targets and Units to a process with different access.
	IsServerWorker     bool               `json:",omitempty" immutable:"true" description:"If true, this is a server-hosted worker. It cannot be changed after the worker is created."`
	UseUserIdentity    bool               `json:",omitempty" immutable:"true" description:"If true, the server worker operates using the requesting user's identity rather than the worker's bot identity. Requires IsServerWorker to be true. It cannot be changed after the worker is created."`
	FunctionWorkerInfo FunctionWorkerInfo `description:"FunctionWorker capabilities"`
}
