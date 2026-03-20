// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package common

import "github.com/confighub/sdk/core/worker/api"

// SendErrorStatus sends an error status through the worker context
func SendErrorStatus(wctx api.BridgeWorkerContext, message string) error {
	status := &api.ActionResult{
		ActionResultBaseMeta: api.ActionResultBaseMeta{
			Status:  api.ActionStatusFailed,
			Result:  api.ActionResultApplyFailed,
			Message: message,
		},
	}
	return wctx.SendStatus(status)
}

func NewActionResult(status api.ActionStatusType, result api.ActionResultType, message string) *api.ActionResult {
	return &api.ActionResult{
		ActionResultBaseMeta: api.ActionResultBaseMeta{
			Status:  status,
			Result:  result,
			Message: message,
		},
	}
}
