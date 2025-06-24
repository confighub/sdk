// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

// This example shows how to register and use a custom function with ConfigHub.
// This would typically be done in a bridge worker or function server.
package main

import (
	"log"
	"os"

	function "github.com/confighub/sdk/function"
	"github.com/confighub/sdk/function/handler"
	"github.com/confighub/sdk/worker"
	"github.com/confighub/sdk/workerapi"
)

func main() {
	// Use the following instead if you want an empty executor with just the custom function registered:
	// executor := function.NewEmptyExecutor()

	executor := function.NewStandardExecutor()
	executor.RegisterFunction(workerapi.ToolchainKubernetesYAML, handler.FunctionRegistration{
		FunctionSignature: GetHelloWorldFunctionSignature(),
		Function:          HelloWorldFunction,
	})

	connector, err := worker.NewConnector(worker.ConnectorOptions{
		WorkerID:         os.Getenv("CONFIGHUB_WORKER_ID"),
		WorkerSecret:     os.Getenv("CONFIGHUB_WORKER_SECRET"),
		ConfigHubURL:     os.Getenv("CONFIGHUB_URL"),
		FunctionExecutor: executor,
	})

	if err != nil {
		log.Fatalf("Failed to create connector: %v", err)
	}

	err = connector.Start()
	if err != nil {
		log.Fatalf("Failed to start connector: %v", err)
	}
}
