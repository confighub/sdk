// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package api

type WorkerEventRequest struct {
	Action  ActionType `description:"The action requested"`
	Payload WorkerPayload
}

type WorkerPayload struct {
	Timestamp int64 `description:"Time the action was requested"`
}
