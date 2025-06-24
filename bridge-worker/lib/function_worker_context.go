// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package lib

import (
	"context"

	"github.com/confighub/sdk/bridge-worker/api"
)

type defaultFunctionWorkerContext struct {
	ctx        context.Context
	serverURL  string
	workerID   string
	sendResult func(*api.ActionResult) error
}

func (d *defaultFunctionWorkerContext) Context() context.Context {
	return d.ctx
}
