// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package lib

import (
	"context"
)

// TransportType selects the wire protocol the worker uses to talk to the
// ConfigHub server.
//
// TransportHTTP2Stream uses the long-lived h2c stream that ships in v1 and is
// the default; its implementation lives inline in workerClient (client.go).
// TransportLongPoll uses HTTP long-polling on the main API port, implemented
// by longPollTransport (transport_long_poll.go).
type TransportType string

const (
	TransportHTTP2Stream TransportType = "http2-stream"
	TransportLongPoll    TransportType = "long-poll"
)

// eventDispatcher is implemented by workerClient. The long-poll transport
// calls it when events arrive from the server and when the connection state
// changes.
type eventDispatcher interface {
	// DispatchEvent hands an event off to the queue manager / watcher
	// dispatch. The transport calls this once per inbound event after
	// it has stripped wire framing.
	DispatchEvent(ctx context.Context, eventType string, data []byte)

	// SetServing reports whether the worker is currently inside the
	// inbound event loop. Probe handlers read this via Worker.IsServing.
	SetServing(serving bool)
}
