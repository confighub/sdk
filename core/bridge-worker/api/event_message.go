// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package api

// SSE event types
const (
	// EventWorker is the event type for worker-level events.
	EventWorker = "WorkerEvent"

	// EventBridgeWorker is the event type for bridge worker-level events.
	EventBridgeWorker = "BridgeWorkerEvent"

	// EventFunctionWorker is the event type for function worker-level events.
	EventFunctionWorker = "FunctionWorkerEvent"
)

// This currently matches the HTTP2 server-sent events protocol.

type EventMessage struct {
	Event string
	Data  interface{}
}
