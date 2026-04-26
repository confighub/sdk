// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package lib

import (
	"context"
	"errors"
	"log"
	"sync/atomic"
	"time"

	"github.com/confighub/sdk/core/worker/api"
	"github.com/confighub/sdk/core/function/executor"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"
)

type Worker struct {
	confighubURL     string
	workerId         string
	workerSecret     string
	bridgeWorker     api.BridgeWorker
	functionExecutor executor.FunctionExecutor
	// client is set in Start; reads from probe handlers may race with that
	// assignment, so use atomic.Pointer for safe publication.
	client       atomic.Pointer[workerClient]
	metricsMeter metric.Meter
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
	if c := b.client.Load(); c != nil {
		c.WaitForPendingOperations()
	}
}

// LastEventHandledAt returns the time the worker most recently handled an
// event from the ConfigHub server's stream. Returns the zero time if the
// worker has not started or has not yet handled an event.
func (b *Worker) LastEventHandledAt() time.Time {
	c := b.client.Load()
	if c == nil {
		return time.Time{}
	}
	return c.LastEventHandledAt()
}

// IsServing reports whether the worker's event-stream loop is currently
// running. Returns false before Start and after WaitForPendingOperations.
func (b *Worker) IsServing() bool {
	c := b.client.Load()
	if c == nil {
		return false
	}
	return c.IsServing()
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
	b.client.Store(client)

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
	if err := client.Start(subCtx); err != nil {
		log.Printf("Error starting worker: %v", err)
		return err
	}
	return nil
}
