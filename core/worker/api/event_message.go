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

	// EventLog is the message type carrying one EventLogEntry: a fact delivered
	// from ConfigHub's event log to a subscribed worker. Its Data is an
	// EventLogEntry.
	EventLog = "EventLog"
)

// This currently matches the HTTP2 server-sent events protocol.

type EventMessage struct {
	Event string
	Data  interface{}
}
