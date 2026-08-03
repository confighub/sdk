// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package worker

// This is the main entry point for the ConfigHub Connector.
// ConfigHub Connector is responsible for connecting to ConfigHub and receiving function invocation events.
// It will register functions in ConfigHub based on what is registered in the FunctionExecutor.

import (
	"context"

	"github.com/confighub/sdk/core/function/executor"
	"github.com/confighub/sdk/core/worker/api"
	"github.com/confighub/sdk/core/worker/lib"
)

// EventHandler re-exports lib.EventHandler so bot authors importing this package
// don't also have to import core/worker/lib to register an event callback.
type EventHandler = lib.EventHandler

// EventConsumer re-exports lib.EventConsumer, the standalone event-log reader.
// It is separate from the worker connection: an event consumer authenticates as
// a worker identity but holds no lease and dequeues no operations.
type EventConsumer = lib.EventConsumer

// NewEventConsumer creates a standalone event-log consumer for the given worker
// identity and one subscription (its Name is the cursor name). handler is invoked
// for each delivered fact. Call Run to start consuming; it blocks until its
// context is canceled. Run several consumers for several cursors.
func NewEventConsumer(serverURL, workerID, workerSecret string, subscription api.EventSubscription, handler EventHandler) *EventConsumer {
	return lib.NewEventConsumer(serverURL, workerID, workerSecret, subscription, handler)
}

type ConfighubConnector struct {
	functionExecutor executor.FunctionExecutor
	workerID         string
	workerSecret     string
	configHubURL     string
}

type ConnectorOptions struct {
	WorkerID         string
	WorkerSecret     string
	ConfigHubURL     string
	FunctionExecutor executor.FunctionExecutor
}

// NewConnector creates a new ConfighubConnector. WorkerID and WorkerSecret are required.
// The rest of the configuration is loaded from ConfigHub after the worker connects.
func NewConnector(opts ConnectorOptions) (*ConfighubConnector, error) {
	return &ConfighubConnector{
		functionExecutor: opts.FunctionExecutor,
		workerID:         opts.WorkerID,
		workerSecret:     opts.WorkerSecret,
		configHubURL:     opts.ConfigHubURL,
	}, nil
}

// Start starts the worker. It opens a persistent connection to ConfigHub and starts performing work based on its configuration.
//
// The worker long-polls the main API port; there is no separate worker port to
// discover.
func (c *ConfighubConnector) Start() error {
	worker := lib.New(c.configHubURL, c.workerID, c.workerSecret).
		WithFunctionExecutor(c.functionExecutor)

	return worker.Start(context.Background())
}
