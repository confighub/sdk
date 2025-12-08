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
	case api.ActionCancel:
		// Cancel operations don't perform work, they signal to cancel in-flight operations
		log.Printf("📥 Received CANCEL command for operation %s", op.Payload.QueuedOperationID)

		// Cancel the operation
		success := c.unitQueues.CancelOperation(op.Payload.QueuedOperationID)

		// Send acknowledgment
		startedAt := time.Now()
		terminatedAt := startedAt
		status := api.ActionStatusCompleted
		message := "Operation cancelled"
		if !success {
			status = api.ActionStatusFailed
			message = "Operation not found or already completed"
		}

		result := &api.ActionResult{
			UnitID:            op.Payload.UnitID,
			SpaceID:           op.Payload.SpaceID,
			QueuedOperationID: op.Payload.QueuedOperationID,
			ActionResultBaseMeta: api.ActionResultBaseMeta{
				Action:       api.ActionCancel,
				Result:       api.ActionResultNone,
				Status:       status,
				Message:      message,
				StartedAt:    startedAt,
				TerminatedAt: &terminatedAt,
			},
		}
		return c.sendResult(result)
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
		// Use SubmitWatcherAndWait to keep the running operation registered until watcher completes.
		// This enables Apply override detection during the watch phase.
		c.watcherManager.SubmitWatcherAndWait(workerContext.Context(), payload.UnitID, api.ActionApply, func(watcherCtx context.Context) {
			// Create a new context that uses the watcher context
			watchContext := &defaultBridgeWorkerContext{
				ctx:        watcherCtx,
				sendResult: workerContext.SendStatus,
			}

			operation := func() (any, error) {
				// Check if context was canceled before starting
				select {
				case <-watcherCtx.Done():
					log.Printf("⚠️ Apply watcher for unit %s canceled", payload.UnitID)
					return nil, watcherCtx.Err()
				default:
				}
				return nil, watchable.WatchForApply(watchContext, payload)
			}
			eb := &backoff.ExponentialBackOff{
				InitialInterval:     30 * time.Second,
				RandomizationFactor: backoff.DefaultRandomizationFactor, /* 0.5 */
				Multiplier:          backoff.DefaultMultiplier,          /* 1.5 */
				MaxInterval:         5 * time.Minute,
			}
			_, err := backoff.Retry(
				watcherCtx,
				operation,
				backoff.WithBackOff(eb),
			)
			if err != nil {
				if errors.Is(err, context.Canceled) {
					log.Printf("✅ Apply watcher for unit %s was successfully canceled", payload.UnitID)
				} else {
					log.Printf("❌ Error watching for apply unit %s: %v", payload.UnitID, err)
				}
			} else {
				log.Printf("✅ Apply watcher for unit %s completed successfully", payload.UnitID)
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
		// Use SubmitWatcherAndWait to keep the running operation registered until watcher completes.
		// This enables Destroy override detection during the watch phase.
		c.watcherManager.SubmitWatcherAndWait(workerContext.Context(), payload.UnitID, api.ActionDestroy, func(watcherCtx context.Context) {
			// Create a new context that uses the watcher context
			watchContext := &defaultBridgeWorkerContext{
				ctx:        watcherCtx,
				sendResult: workerContext.SendStatus,
			}

			operation := func() (any, error) {
				// Check if context was canceled before starting
				select {
				case <-watcherCtx.Done():
					log.Printf("⚠️ Destroy watcher for unit %s canceled", payload.UnitID)
					return nil, watcherCtx.Err()
				default:
				}
				return nil, watchable.WatchForDestroy(watchContext, payload)
			}
			eb := &backoff.ExponentialBackOff{
				InitialInterval:     30 * time.Second,
				RandomizationFactor: backoff.DefaultRandomizationFactor,
				Multiplier:          backoff.DefaultMultiplier,
				MaxInterval:         5 * time.Minute,
			}
			_, err := backoff.Retry(
				watcherCtx,
				operation,
				backoff.WithBackOff(eb),
			)
			if err != nil {
				if errors.Is(err, context.Canceled) {
					log.Printf("✅ Destroy watcher for unit %s was successfully canceled", payload.UnitID)
				} else {
					log.Printf("❌ Error watching for destroy unit %s: %v", payload.UnitID, err)
				}
			} else {
				log.Printf("✅ Destroy watcher for unit %s completed successfully", payload.UnitID)
			}
		})
	}
	return nil
}

func (c *workerClient) handleFinalize(workerContext api.BridgeWorkerContext, payload api.BridgeWorkerPayload) error {
	log.Printf("📥 Received FINALIZE command with payload: %s", string(payload.Data))
	return c.bridgeWorker.Finalize(workerContext, payload)
}
