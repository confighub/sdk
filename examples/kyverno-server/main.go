// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

// This example shows how to validate Kubernetes resources against Kyverno policies
// running in a cluster by sending AdmissionReview requests to the Kyverno webhook.
package main

import (
	"log"
	"os"

	"github.com/confighub/sdk/configkit/k8skit"
	"github.com/confighub/sdk/function/executor"
	"github.com/confighub/sdk/function/handler"
	"github.com/confighub/sdk/worker"
	"github.com/confighub/sdk/workerapi"
)

func main() {
	exec := executor.NewEmptyExecutor()
	exec.RegisterToolchain(k8skit.NewK8sResourceProvider())
	exec.RegisterFunction(workerapi.ToolchainKubernetesYAML, handler.FunctionRegistration{
		FunctionSignature: GetVetKyvernoServerSignature(),
		Function:          VetKyvernoServerFunction,
	})

	connector, err := worker.NewConnector(worker.ConnectorOptions{
		WorkerID:         os.Getenv("CONFIGHUB_WORKER_ID"),
		WorkerSecret:     os.Getenv("CONFIGHUB_WORKER_SECRET"),
		ConfigHubURL:     os.Getenv("CONFIGHUB_URL"),
		FunctionExecutor: exec,
	})

	if err != nil {
		log.Fatalf("Failed to create connector: %v", err)
	}

	err = connector.Start()
	if err != nil {
		log.Fatalf("Failed to start connector: %v", err)
	}
}
