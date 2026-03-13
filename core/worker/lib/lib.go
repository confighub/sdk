// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package lib

import (
	"context"
	"errors"
	"log"

	"github.com/confighub/sdk/worker/api"
	"github.com/confighub/sdk/function/executor"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"
)

type Worker struct {
	confighubURL     string
	workerId         string
	workerSecret     string
	bridgeWorker     api.BridgeWorker
	functionExecutor executor.FunctionExecutor
	client           *workerClient
	metricsMeter     metric.Meter
}

func New(url, id, secret string) *Worker {
	return &Worker{
		confighubURL: url,
		workerId:     id,
		workerSecret: secret,
		metricsMeter: noop.Meter{},
	}
}

func (b *Worker) WithBridgeWorker(bridgeWorker api.BridgeWorker) *Worker {
	b.bridgeWorker = bridgeWorker
	return b
}

func (b *Worker) WithFunctionExecutor(functionExecutor executor.FunctionExecutor) *Worker {
	b.functionExecutor = functionExecutor
	return b
}

func (b *Worker) WithMetricsMeter(meter metric.Meter) *Worker {
	b.metricsMeter = meter
	return b
}

// WaitForPendingOperations waits for all in-flight operations to complete
func (b *Worker) WaitForPendingOperations() {
	if b.client != nil {
		b.client.WaitForPendingOperations()
	}
}

func (b *Worker) Start(ctx context.Context) error {
	client := newClient(
		b.confighubURL,
		b.workerId,
		b.workerSecret,
		b.bridgeWorker,
		b.functionExecutor,
		b.metricsMeter,
	)
	b.client = client

	subCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Start error monitoring goroutine for queue errors
	go func() {
		for err := range client.unitQueues.ErrorChannel() {
			log.Printf("[QUEUE ERROR] %v", err)
		}
	}()

	if len(b.workerSecret) < 8 {
		if len(b.workerSecret) == 0 {
			log.Printf("No worker secret")
		} else {
			log.Printf("Invalid worker secret")
		}
		return errors.New("missing or invalid worker secret")
	}
	log.Printf("Starting worker with ID: %s", b.workerId)
	log.Printf("Starting worker with Token: %s...", b.workerSecret[:8])
	if err := b.client.Start(subCtx); err != nil {
		log.Printf("Error starting worker: %v", err)
		return err
	}
	return nil
}
