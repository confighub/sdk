// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package lib

import (
	"context"
	"time"

	"github.com/confighub/sdk/bridge-worker/api"
)

type defaultBridgeWorkerContext struct {
	ctx        context.Context
	serverURL  string
	workerID   string
	sendResult func(*api.ActionResult) error
}

var _ api.BridgeWorkerContext = (*defaultBridgeWorkerContext)(nil)

func (d *defaultBridgeWorkerContext) Context() context.Context {
	return d.ctx
}

func (d *defaultBridgeWorkerContext) GetServerURL() string {
	return d.serverURL
}

func (d *defaultBridgeWorkerContext) GetWorkerID() string {
	return d.workerID
}

func (d *defaultBridgeWorkerContext) SendStatus(result *api.ActionResult) error {
	// TerminatedAt is set only the result is complete or failed (not None)
	if result.Result != api.ActionResultNone &&
		result.Result != api.ActionResultApplyWaitFailed {
		t := time.Now()
		result.TerminatedAt = &t
	}
	return d.sendResult(result)
}
