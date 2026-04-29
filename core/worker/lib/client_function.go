// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package lib

import (
	"encoding/json"
	"log"
	"time"

	"github.com/confighub/sdk/core/worker/api"
)

// processFunctionCommand handles commands specifically for the function worker.
func (c *workerClient) processFunctionCommand(ctx *defaultFunctionWorkerContext, reqEvent api.FunctionWorkerEventRequest) error {
	log.Printf("Received function worker command: Action=%s, QueuedOperationID=%s", reqEvent.Action, reqEvent.Payload.QueuedOperationID)

	// Handle cancel action
	if reqEvent.Action == api.ActionCancel {
		log.Printf("📥 Received CANCEL command for operation %s", reqEvent.Payload.QueuedOperationID)

		// Cancel the operation
		success := c.unitQueues.CancelOperation(reqEvent.Payload.QueuedOperationID)

		// Send acknowledgment. A successful cancel sends Canceled (not Completed)
		// so callers can distinguish "we aborted the work" from "the work finished".
		startedAt := time.Now()
		terminatedAt := startedAt
		status := api.ActionStatusCanceled
		message := "Operation cancelled"
		if !success {
			status = api.ActionStatusFailed
			message = "Operation not found or already completed"
		}

		result := &api.ActionResult{
			UnitID:            reqEvent.Payload.InvocationRequest.UnitID,
			SpaceID:           reqEvent.Payload.InvocationRequest.SpaceID,
			QueuedOperationID: reqEvent.Payload.QueuedOperationID,
			ActionResultBaseMeta: api.ActionResultBaseMeta{
				Action:       api.ActionCancel,
				Result:       api.ActionResultNone,
				Status:       status,
				Message:      message,
				StartedAt:    startedAt,
				TerminatedAt: &terminatedAt,
			},
		}
		return c.sendResult(result)
	}

	startTime := time.Now()

	resp, err := c.functionExecutor.Invoke(ctx.Context(), &reqEvent.Payload.InvocationRequest)
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

	resBytes, err := json.Marshal(resp)
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
