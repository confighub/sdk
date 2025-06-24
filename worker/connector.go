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

	"github.com/confighub/sdk/bridge-worker/api"
	"github.com/confighub/sdk/bridge-worker/lib"
	"github.com/confighub/sdk/function"
	funcApi "github.com/confighub/sdk/function/api"
	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
)

type ConfighubConnector struct {
	functionExecutor *function.FunctionExecutor
	workerID         string
	workerSecret     string
	configHubURL     string
}

type ConnectorOptions struct {
	WorkerID         string
	WorkerSecret     string
	ConfigHubURL     string
	FunctionExecutor *function.FunctionExecutor
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
func (c *ConfighubConnector) Start() error {

	// get api info from confighub. First instantiate generated client using confighub url.
	apiClient, err := goclientnew.NewClientWithResponses(c.configHubURL + "/api")
	if err != nil {
		return err
	}
	apiInfo, err := apiClient.ApiInfoWithResponse(context.Background())
	if err != nil {
		return err
	}
	if apiInfo.JSON200 == nil {
		return fmt.Errorf("failed to get api info: %v", apiInfo.Body)
	}
	url, err := url.Parse(c.configHubURL)
	if err != nil {
		return err
	}
	fmt.Println(apiInfo.JSON200.WorkerPort)
	workerUrl := fmt.Sprintf("%s://%s:%s", url.Scheme, url.Hostname(), apiInfo.JSON200.WorkerPort)

	// We create these wrappers so we can reuse the exising worker code in lib.worker.
	// We don't support bridges yet in this first iteration, so it's just a null placeholder.
	// Ultimately the whole thing should be refactored.
	bridgeWorker := &NullBridgeWorker{}
	adapter := &FunctionWorkerAdapter{executor: c.functionExecutor}

	worker := lib.New(workerUrl, c.workerID, c.workerSecret).
		WithBridgeWorker(bridgeWorker).
		WithFunctionWorker(adapter)

	return worker.Start(context.Background())
}

type NullBridgeWorker struct{}

func (n *NullBridgeWorker) Info(opts api.InfoOptions) api.BridgeWorkerInfo {
	return api.BridgeWorkerInfo{
		SupportedConfigTypes: []*api.ConfigType{},
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

type FunctionWorkerAdapter struct {
	executor *function.FunctionExecutor
}

func (a *FunctionWorkerAdapter) Info() api.FunctionWorkerInfo {
	return api.FunctionWorkerInfo{
		SupportedFunctions: a.executor.RegisteredFunctions(),
	}
}

func (a *FunctionWorkerAdapter) Invoke(workerCtx api.FunctionWorkerContext, request funcApi.FunctionInvocationRequest) (funcApi.FunctionInvocationResponse, error) {
	resp, err := a.executor.Invoke(workerCtx.Context(), &request)
	if err != nil {
		return funcApi.FunctionInvocationResponse{}, err
	}
	if resp == nil {
		return funcApi.FunctionInvocationResponse{}, nil
	}
	return *resp, nil
}
