// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package lib

import (
	"encoding/json"
	"log"
	"time"

	"github.com/confighub/sdk/bridge-worker/api"
)

// processFunctionCommand handles commands specifically for the function worker.
// TODO: Implement the actual logic for function execution.
func (c *workerClient) processFunctionCommand(ctx *defaultFunctionWorkerContext, reqEvent api.FunctionWorkerEventRequest) error {
	log.Printf("Received function worker command: Action=%s, QueuedOperationID=%s", reqEvent.Action, reqEvent.Payload.QueuedOperationID)

	startTime := time.Now()

	res, err := c.functionWorker.Invoke(ctx, reqEvent.Payload.InvocationRequest)
	if err != nil {
		// send back ActionResult with invocation failure
		failureResult := &api.ActionResult{
			QueuedOperationID: reqEvent.Payload.QueuedOperationID,
			UnitID:            reqEvent.Payload.InvocationRequest.UnitID,
			ActionResultBaseMeta: api.ActionResultBaseMeta{
				Action:      reqEvent.Action,
				Status:      api.ActionStatusFailed,
				Result:      api.ActionResultFunctionInvocationFailed,
				Message:     err.Error(),
				RevisionNum: reqEvent.Payload.InvocationRequest.RevisionNum,
			},
		}
		_ = c.sendResult(failureResult)
		return err
	}

	resBytes, err := json.Marshal(res)
	if err != nil {
		// send back ActionResult with invocation failure
		failureResult := &api.ActionResult{
			QueuedOperationID: reqEvent.Payload.QueuedOperationID,
			UnitID:            reqEvent.Payload.InvocationRequest.UnitID,
			SpaceID:           reqEvent.Payload.InvocationRequest.SpaceID,
			ActionResultBaseMeta: api.ActionResultBaseMeta{
				Action:      reqEvent.Action,
				Status:      api.ActionStatusFailed,
				Result:      api.ActionResultFunctionInvocationFailed,
				Message:     err.Error(),
				RevisionNum: reqEvent.Payload.InvocationRequest.RevisionNum,
			},
		}
		_ = c.sendResult(failureResult)
		return err
	}

	terminatedAt := time.Now()
	result := &api.ActionResult{
		QueuedOperationID: reqEvent.Payload.QueuedOperationID,
		UnitID:            reqEvent.Payload.InvocationRequest.UnitID,
		SpaceID:           reqEvent.Payload.InvocationRequest.SpaceID,
		ActionResultBaseMeta: api.ActionResultBaseMeta{
			Action:       reqEvent.Action,
			Status:       api.ActionStatusCompleted,
			Result:       api.ActionResultFunctionInvocationCompleted,
			Message:      "Function invocation completed",
			RevisionNum:  reqEvent.Payload.InvocationRequest.RevisionNum,
			StartedAt:    startTime,
			TerminatedAt: &terminatedAt,
		},
		Data: resBytes,
	}

	return c.sendResult(result)
}
