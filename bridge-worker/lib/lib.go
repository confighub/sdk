// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package lib

import (
	"context"
	"errors"
	"log"

	"github.com/confighub/sdk/bridge-worker/api"
)

type Worker struct {
	confighubURL   string
	workerId       string
	workerSecret   string
	bridgeWorker   api.BridgeWorker
	functionWorker api.FunctionWorker
	client         *workerClient
}

func New(url, id, secret string) *Worker {
	return &Worker{
		confighubURL: url,
		workerId:     id,
		workerSecret: secret,
	}
}

func (b *Worker) WithBridgeWorker(bridgeWorker api.BridgeWorker) *Worker {
	b.bridgeWorker = bridgeWorker
	return b
}

func (b *Worker) WithFunctionWorker(functionWorker api.FunctionWorker) *Worker {
	b.functionWorker = functionWorker
	return b
}

func (b *Worker) Start(ctx context.Context) error {
	client := newClient(b.confighubURL, b.workerId, b.workerSecret, b.bridgeWorker, b.functionWorker)
	b.client = client

	subCtx, cancel := context.WithCancel(ctx)
	defer cancel()

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
