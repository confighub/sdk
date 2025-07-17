// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

// This example shows how to build a custom worker with a custom bridge for ConfigHub.
package main

import (
	"log"
	"os"

	"github.com/confighub/sdk/worker"
)

func main() {
	// Note: The SDK currently uses controller-runtime logging internally.
	// Until the SDK provides a way to configure logging, you may see warnings about
	// uninitialized loggers. These can be safely ignored for this example.

	// For your own logging, you can use standard log package as shown in this example
	log.Printf("[INFO] Starting hello-world-bridge example...")

	// The dispatcher can (theoretically) be used in a standalone model without the connector.
	// But it has not been tested yet.
	bridgeDispatcher := worker.NewBridgeDispatcher()

	// For this example the bridge is a basic filesystem bridge. It illustrates the example that
	// the core config for bridges is often provided by environment variables.
	baseDir := os.Getenv("EXAMPLE_BRIDGE_DIR")
	if baseDir == "" {
		baseDir = "/tmp/confighub-example-bridge"
	}
	log.Printf("[INFO] Using base directory: %s", baseDir)

	// The bridge is implemented in example_bridge.go.
	bridge, err := NewExampleBridge("example-bridge", baseDir)
	if err != nil {
		log.Fatalf("Failed to create bridge: %v", err)
	}
	bridgeDispatcher.RegisterBridge(bridge)

	// The connector is the "engine" of the worker. You register bridges and functions with it.
	// Then you start it and it connects to ConfigHub and offers its local capabilities to your ConfigHub org.
	connector, err := worker.NewConnector(worker.ConnectorOptions{
		WorkerID:         os.Getenv("CONFIGHUB_WORKER_ID"),
		WorkerSecret:     os.Getenv("CONFIGHUB_WORKER_SECRET"),
		ConfigHubURL:     os.Getenv("CONFIGHUB_URL"),
		BridgeDispatcher: &bridgeDispatcher,
	})

	if err != nil {
		log.Fatalf("Failed to create connector: %v", err)
	}

	log.Printf("[INFO] Starting connector...")
	err = connector.Start()
	if err != nil {
		log.Fatalf("Failed to start connector: %v", err)
	}
}
