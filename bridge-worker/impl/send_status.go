// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package impl

import "github.com/confighub/sdk/bridge-worker/api"

// sendErrorStatus sends an error status through the worker context
func sendErrorStatus(wctx api.BridgeWorkerContext, message string) error {
	status := &api.ActionResult{
		ActionResultBaseMeta: api.ActionResultBaseMeta{
			Status:  api.ActionStatusFailed,
			Result:  api.ActionResultApplyFailed,
			Message: message,
		},
	}
	return wctx.SendStatus(status)
}

func newActionResult(status api.ActionStatusType, result api.ActionResultType, message string) *api.ActionResult {
	return &api.ActionResult{
		ActionResultBaseMeta: api.ActionResultBaseMeta{
			Status:  status,
			Result:  result,
			Message: message,
		},
	}
}
