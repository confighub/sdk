// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package lib

import (
	"context"
)

// eventDispatcher is implemented by workerClient. The transport calls it when
// events arrive from the server and when the connection state changes.
type eventDispatcher interface {
	// DispatchEvent hands an event off to the queue manager / watcher
	// dispatch. The transport calls this once per inbound event after
	// it has stripped wire framing.
	DispatchEvent(ctx context.Context, eventType string, data []byte)

	// SetServing reports whether the worker is currently inside the
	// inbound event loop. Probe handlers read this via Worker.IsServing.
	SetServing(serving bool)
}
