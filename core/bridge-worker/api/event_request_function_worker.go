// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package api

import (
	"github.com/google/uuid"

	funcApi "github.com/confighub/sdk/function/api"
)

// FunctionWorkerEventRequest encapsulates a request destined for the function worker plugin.
// It specifies the action to be performed and includes the necessary payload.
type FunctionWorkerEventRequest struct {
	// Action defines the operation the function worker should perform.
	Action ActionType `description:"Action defines the operation the function worker should perform."`

	// Payload contains the data required for the action, primarily the function invocation details.
	Payload FunctionWorkerPayload `description:"Payload contains the data required for the action, primarily the function invocation details."`
}

// FunctionWorkerPayload holds the specific data for a function worker request.
type FunctionWorkerPayload struct {
	// QueuedOperationID is the identifier of the original queued operation request.
	QueuedOperationID uuid.UUID `description:"QueuedOperationID is the identifier of the original queued operation request."`

	// InvocationRequest encapsulates the full request for invoking one or more functions,
	// including context, configuration data, function calls, and execution options.
	// This structure is defined in the functions plugin's API package.
	InvocationRequest funcApi.FunctionInvocationRequest `description:"InvocationRequest encapsulates the full request for invoking one or more functions, including context, configuration data, function calls, and execution options."`
}
