// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

// This example shows how to register and use a Kyverno policy validation function with ConfigHub.
package main

import (
	"log"
	"os"

	funcimpl "github.com/confighub/sdk/function-impl"
	"github.com/confighub/sdk/function/handler"
	"github.com/confighub/sdk/worker"
	"github.com/confighub/sdk/workerapi"
)

func main() {
	executor := funcimpl.NewStandardExecutor()
	executor.RegisterFunction(workerapi.ToolchainKubernetesYAML, handler.FunctionRegistration{
		FunctionSignature: GetVetKyvernoSignature(),
		Function:          VetKyvernoFunction,
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
