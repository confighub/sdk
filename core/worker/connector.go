// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package worker

// This is the main entry point for the ConfigHub Connector.
// ConfigHub Connector is responsible for connecting to ConfigHub and receiving function invocation events.
// It will register functions in ConfigHub based on what is registered in the FunctionExecutor.

// While the connector is right now focused on function execution, it is expected that we will evolve the code
// in the SDK module to have a simple and clear public API like this one for the full worker functionality,
// including bridge functionality.

import (
	"context"
	"fmt"
	"net/url"

	"github.com/confighub/sdk/core/function/executor"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/confighub/sdk/core/worker/api"
	"github.com/confighub/sdk/core/worker/lib"
)

// TransportType re-exports lib.TransportType so custom-worker authors who
// import this package don't also have to import core/worker/lib just to
// pick a transport.
type TransportType = lib.TransportType

const (
	TransportHTTP2Stream = lib.TransportHTTP2Stream
	TransportLongPoll    = lib.TransportLongPoll
)

// EventHandler re-exports lib.EventHandler so bot authors importing this package
// don't also have to import core/worker/lib to register an event callback.
type EventHandler = lib.EventHandler

// EventConsumer re-exports lib.EventConsumer, the standalone event-log reader.
// It is separate from the bridge/function worker connection: an event consumer
// authenticates as a worker identity but holds no lease and dequeues no
// operations.
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
	bridgeDispatcher *BridgeDispatcher
	workerID         string
	workerSecret     string
	configHubURL     string
	transport        TransportType
}

type ConnectorOptions struct {
	WorkerID         string
	WorkerSecret     string
	ConfigHubURL     string
	FunctionExecutor executor.FunctionExecutor
	BridgeDispatcher *BridgeDispatcher
	// Transport selects the wire protocol. Defaults to TransportHTTP2Stream
	// (the v1 long-lived h2c stream). Set to TransportLongPoll to use the
	// HTTP long-polling transport, which talks to the main API port and does
	// not require a separate worker port.
	Transport TransportType
}

// NewConnector creates a new ConfighubConnector. WorkerID and WorkerSecret are required.
// The rest of the configuration is loaded from ConfigHub after the worker connects.
func NewConnector(opts ConnectorOptions) (*ConfighubConnector, error) {
	transport := opts.Transport
	if transport == "" {
		transport = TransportHTTP2Stream
	}
	return &ConfighubConnector{
		functionExecutor: opts.FunctionExecutor,
		bridgeDispatcher: opts.BridgeDispatcher,
		workerID:         opts.WorkerID,
		workerSecret:     opts.WorkerSecret,
		configHubURL:     opts.ConfigHubURL,
		transport:        transport,
	}, nil
}

// Start starts the worker. It opens a persistent connection to ConfigHub and starts performing work based on its configuration.
func (c *ConfighubConnector) Start() error {
	connectURL, err := c.resolveConnectURL()
	if err != nil {
		return err
	}

	var bw api.BridgeWorker // api.BridgeWorker is the interface.
	if c.bridgeDispatcher != nil {
		bw = c.bridgeDispatcher.getWrapped()
	} else {
		bw = &NullBridgeWorker{}
	}

	worker := lib.New(connectURL, c.workerID, c.workerSecret).
		WithBridgeWorker(bw).
		WithFunctionExecutor(c.functionExecutor).
		WithTransport(c.transport)

	return worker.Start(context.Background())
}

// resolveConnectURL returns the URL the worker SDK should be initialized
// with. For the SSE/h2c stream transport that's the WorkerPort variant
// (advertised by /api/info); the long-poll transport stays on the main API
// port so it doesn't need the dedicated h2c port at all.
func (c *ConfighubConnector) resolveConnectURL() (string, error) {
	if c.transport == TransportLongPoll {
		// Long-poll endpoints (/api/space/.../lease, .../queued_operation/*)
		// live on the main router only; switching to WorkerPort would 404.
		// Use the configured URL as-is.
		return c.configHubURL, nil
	}

	apiClient, err := goclientnew.NewClientWithResponses(c.configHubURL + "/api")
	if err != nil {
		return "", err
	}
	apiInfo, err := apiClient.ApiInfoWithResponse(context.Background())
	if err != nil {
		return "", err
	}
	if apiInfo.JSON200 == nil {
		return "", fmt.Errorf("failed to get api info: %v", apiInfo.Body)
	}
	parsed, err := url.Parse(c.configHubURL)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s://%s:%s", parsed.Scheme, parsed.Hostname(), apiInfo.JSON200.WorkerPort), nil
}

type NullBridgeWorker struct{}

func (n *NullBridgeWorker) ID() api.BridgeWorkerID {
	return api.BridgeWorkerID{}
}

func (n *NullBridgeWorker) Info(opts api.InfoOptions) api.BridgeWorkerInfo {
	return api.BridgeWorkerInfo{
		SupportedConfigTypes: []*api.SupportedConfigType{},
	}
}

func (n *NullBridgeWorker) Apply(wctx api.BridgeWorkerContext, payload api.BridgeWorkerPayload) error {
	return nil
}

func (n *NullBridgeWorker) Destroy(wctx api.BridgeWorkerContext, payload api.BridgeWorkerPayload) error {
	return nil
}

func (n *NullBridgeWorker) Refresh(wctx api.BridgeWorkerContext, payload api.BridgeWorkerPayload) error {
	return nil
}

func (n *NullBridgeWorker) Finalize(wctx api.BridgeWorkerContext, payload api.BridgeWorkerPayload) error {
	return nil
}

func (n *NullBridgeWorker) Import(wctx api.BridgeWorkerContext, payload api.BridgeWorkerPayload) error {
	return nil
}
