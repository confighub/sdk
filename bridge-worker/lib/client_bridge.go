// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package lib

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/cenkalti/backoff/v5"
	"github.com/confighub/sdk/bridge-worker/api"
)

func (c *workerClient) processBridgeCommand(workerContext *defaultBridgeWorkerContext, op api.BridgeWorkerEventRequest) error {
	// Helper to configure the sendResult callback with common metadata.
	setupSendResult := func(action api.ActionType) {
		startedAt := time.Now()
		workerContext.sendResult = func(r *api.ActionResult) error {
			if r == nil {
				return errors.New("nil action result")
			}
			r.Action = action
			r.StartedAt = startedAt
			r.UnitID = op.Payload.UnitID
			r.SpaceID = op.Payload.SpaceID
			r.RevisionNum = op.Payload.RevisionNum
			r.QueuedOperationID = op.Payload.QueuedOperationID
			return c.sendResult(r)
		}
	}

	switch action := op.Action; action {
	case api.ActionApply:
		setupSendResult(api.ActionApply)
		watch, err := c.handleApply(workerContext, op.Payload)
		if err != nil {
			return err
		}
		if watch {
			return c.handleWatchApply(workerContext, op.Payload)
		}
		return nil
	case api.ActionRefresh:
		setupSendResult(api.ActionRefresh)
		return c.handleGet(workerContext, op.Payload)
	case api.ActionImport:
		setupSendResult(api.ActionImport)
		return c.handleImport(workerContext, op.Payload)
	case api.ActionDestroy:
		setupSendResult(api.ActionDestroy)
		watch, err := c.handleDestroy(workerContext, op.Payload)
		if err != nil {
			return err
		}
		if watch {
			return c.handleWatchDestroy(workerContext, op.Payload)
		}
		return nil
	case api.ActionFinalize:
		setupSendResult(api.ActionFinalize)
		return c.handleFinalize(workerContext, op.Payload)
	default:
		// For unknown actions, construct an error result and send it.
		startedAt := time.Now()
		status := &api.ActionResult{
			// The IDs are filled in by the sendResult function
			// UnitID:            op.Payload.UnitID,
			// SpaceID:           op.Payload.SpaceID,
			// QueuedOperationID: op.Payload.QueuedOperationID,
			ActionResultBaseMeta: api.ActionResultBaseMeta{
				Action:       action,
				Result:       api.ActionResultNone,
				Status:       api.ActionStatusNone,
				Message:      fmt.Sprintf("unknown operation name: %s", string(op.Action)),
				StartedAt:    startedAt,
				TerminatedAt: nil,
			},
		}
		return c.sendResult(status)
	}
}

func (c *workerClient) handleApply(workerContext api.BridgeWorkerContext, payload api.BridgeWorkerPayload) (bool, error) {
	log.Printf("📥 Received APPLY command with data: %s", string(payload.Data))
	return true, c.bridgeWorker.Apply(workerContext, payload)
}

func (c *workerClient) handleWatchApply(workerContext api.BridgeWorkerContext, payload api.BridgeWorkerPayload) error {
	log.Printf("📥 Kick off watching for apply")
	if watchable, ok := c.bridgeWorker.(api.WatchableWorker); ok {
		c.watcherPool.Submit(func() {
			operation := func() (any, error) {
				return nil, watchable.WatchForApply(workerContext, payload)
			}
			eb := &backoff.ExponentialBackOff{
				InitialInterval:     30 * time.Second,
				RandomizationFactor: backoff.DefaultRandomizationFactor, /* 0.5 */
				Multiplier:          backoff.DefaultMultiplier,          /* 1.5 */
				MaxInterval:         5 * time.Minute,
			}
			_, err := backoff.Retry(
				context.TODO(),
				operation,
				backoff.WithBackOff(eb),
			)
			if err != nil {
				log.Printf("Error watching for apply: %v", err)
			}
		})
	}
	return nil
}

func (c *workerClient) handleGet(workerContext api.BridgeWorkerContext, payload api.BridgeWorkerPayload) error {
	log.Printf("📥 Received GET command with data: %s", string(payload.Data))
	return c.bridgeWorker.Refresh(workerContext, payload)
}

func (c *workerClient) handleImport(workerContext api.BridgeWorkerContext, payload api.BridgeWorkerPayload) error {
	log.Printf("📥 Received IMPORT command with data: %s", string(payload.Data))
	return c.bridgeWorker.Import(workerContext, payload)
}

func (c *workerClient) handleDestroy(workerContext api.BridgeWorkerContext, payload api.BridgeWorkerPayload) (bool, error) {
	log.Printf("⚠️ Received DESTROY command with data: %s", string(payload.Data))
	return true, c.bridgeWorker.Destroy(workerContext, payload)
}

func (c *workerClient) handleWatchDestroy(workerContext api.BridgeWorkerContext, payload api.BridgeWorkerPayload) error {
	log.Printf("📥 Kick off watching for destroy")
	// TODO rename api.WatchableWorker api.WatchableBridgeWorker
	if watchable, ok := c.bridgeWorker.(api.WatchableWorker); ok {
		c.watcherPool.Submit(func() {
			operation := func() (any, error) {
				return nil, watchable.WatchForDestroy(workerContext, payload)
			}
			eb := &backoff.ExponentialBackOff{
				InitialInterval:     30 * time.Second,
				RandomizationFactor: backoff.DefaultRandomizationFactor,
				Multiplier:          backoff.DefaultMultiplier,
				MaxInterval:         5 * time.Minute,
			}
			_, err := backoff.Retry(
				context.TODO(),
				operation,
				backoff.WithBackOff(eb),
			)
			if err != nil {
				log.Printf("Error watching for destroy: %v", err)
			}
		})
	}
	return nil
}

func (c *workerClient) handleFinalize(workerContext api.BridgeWorkerContext, payload api.BridgeWorkerPayload) error {
	log.Printf("📥 Received FINALIZE command with payload: %s", string(payload.Data))
	return c.bridgeWorker.Finalize(workerContext, payload)
}
