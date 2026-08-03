// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package lib

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/confighub/sdk/core/function/executor"
	"github.com/confighub/sdk/core/worker/api"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

const (
	ErrorTypeUnmarshalWorkerEvent   = "failed_to_unmarshal_worker_event"
	ErrorTypeUnmarshalFunctionEvent = "failed_to_unmarshal_function_event"
	ErrorTypeFunctionCommand        = "failed_to_execute_function_command"
)

type workerClient struct {
	serverURL        string
	workerID         string
	workerSecret     string
	functionExecutor executor.FunctionExecutor
	watcherManager   *WatcherManager // Manages watcher lifecycle and execution
	unitQueues       *UnitQueueManager
	operationsWg     sync.WaitGroup // Track in-flight function invocations
	metricsMeter     metric.Meter
	eventCounter     metric.Int64Counter
	actionCounter    metric.Int64Counter
	errorCounter     metric.Int64Counter

	// longPoll owns every wire concern: auth, claim, poll loop, result
	// submission and reconnect/backoff. See transport_long_poll.go.
	longPoll *longPollTransport
	// dispatchedEvents counts events the transport has handed to DispatchEvent
	// since process start. Used only for log correlation.
	dispatchedEvents uint64

	// eventStateMu guards lastEventHandledAt and serving, which are read by
	// HTTP probe handlers concurrently with the event loop.
	eventStateMu       sync.RWMutex
	lastEventHandledAt time.Time
	serving            bool
}

func newClient(
	serverURL, workerID, workerSecret string,
	functionExecutor executor.FunctionExecutor,
	metricsMeter metric.Meter,
) *workerClient {
	c := &workerClient{
		serverURL:        serverURL,
		workerID:         workerID,
		workerSecret:     workerSecret,
		functionExecutor: functionExecutor,
		watcherManager:   NewWatcherManager(10, 50), // 10 workers, queue size 50
		unitQueues:       NewUnitQueueManager(),
		metricsMeter:     metricsMeter,
	}
	c.longPoll = newLongPollTransport(serverURL, workerID, workerSecret, functionExecutor, c)
	if err := c.initializeEventCounters(); err != nil {
		log.Printf("[WARN] Failed to initialize metric counters: %v", err)
	}
	return c
}

func (c *workerClient) initializeEventCounters() (err error) {
	c.eventCounter, err = c.metricsMeter.Int64Counter("worker.events")
	if err != nil {
		return err
	}

	c.actionCounter, err = c.metricsMeter.Int64Counter("worker.actions")
	if err != nil {
		return err
	}

	c.errorCounter, err = c.metricsMeter.Int64Counter("worker.errors")
	return err
}

// Start runs the worker: it brings up the unit queue manager and watcher
// pool, then hands the connection lifecycle (auth, claim, poll loop,
// reconnect/backoff) to the long-poll transport. See transport_long_poll.go.
//
// Returns ctx.Err() when the context is cancelled, or initial setup errors.
func (c *workerClient) Start(ctx context.Context) error {
	c.unitQueues.Start(ctx)
	defer c.unitQueues.Stop()

	defer func() {
		log.Printf("🛑 Shutting down: stopping all active watchers")
		c.watcherManager.StopAndWait()
	}()

	return c.longPoll.Run(ctx)
}

// DispatchEvent implements eventDispatcher. The transport calls this once per
// inbound event after stripping wire framing; it funnels through
// handleEventWithLogging so metrics, recover(), and "last event handled at"
// bookkeeping apply.
func (c *workerClient) DispatchEvent(ctx context.Context, eventType string, data []byte) {
	n := atomic.AddUint64(&c.dispatchedEvents, 1)
	if err := c.handleEventWithLogging(ctx, eventType, data, int(n)); err != nil {
		log.Printf("[ERROR] Failed to process event #%d: %v", n, err)
	}
}

// SetServing implements eventDispatcher. The transport flips this true while
// its poll loop is up and false when it tears down, so probe handlers reading
// Worker.IsServing see accurate state.
func (c *workerClient) SetServing(serving bool) {
	c.eventStateMu.Lock()
	c.serving = serving
	c.eventStateMu.Unlock()
}

// handleEventWithLogging wraps handleEvent with proper error handling and logging
func (c *workerClient) handleEventWithLogging(ctx context.Context, eventType string, data []byte, eventNumber int) error {
	c.eventStateMu.Lock()
	c.lastEventHandledAt = time.Now()
	c.eventStateMu.Unlock()

	defer func() {
		if r := recover(); r != nil {
			log.Printf("[ERROR] Panic while processing event #%d (type: %s): %v", eventNumber, eventType, r)
		}
	}()

	switch eventType {
	case api.EventWorker, api.EventFunctionWorker:
		if c.eventCounter != nil {
			c.eventCounter.Add(ctx, 1, metric.WithAttributes(
				attribute.String("type", eventType),
			))
		}

		c.handleEvent(ctx, eventType, data)
		return nil
	default:
		if c.eventCounter != nil {
			c.eventCounter.Add(ctx, 1, metric.WithAttributes(
				attribute.String("type", "unknown"),
			))
		}

		return fmt.Errorf("unknown event type: %s", eventType)
	}
}

func (c *workerClient) handleEvent(ctx context.Context, eventType string, data []byte) {
	switch eventType {
	case api.EventWorker:
		var op api.WorkerEventRequest
		if err := json.Unmarshal(data, &op); err != nil {
			if c.errorCounter != nil {
				c.errorCounter.Add(ctx, 1, metric.WithAttributes(
					attribute.String("error", ErrorTypeUnmarshalWorkerEvent),
				))
			}

			log.Printf("[ERROR] Failed to unmarshal worker event: %v", err)
			return
		}

		if c.actionCounter != nil {
			c.actionCounter.Add(ctx, 1, metric.WithAttributes(
				attribute.String("action", string(op.Action)),
			))
		}

		// Non-operation control events carry no work.
		log.Printf("[DEBUG] Received %s worker event", op.Action)
		return

	case api.EventFunctionWorker:
		// Handle events directed to the function worker plugin
		var op api.FunctionWorkerEventRequest
		if err := json.Unmarshal(data, &op); err != nil {
			if c.errorCounter != nil {
				c.errorCounter.Add(ctx, 1, metric.WithAttributes(
					attribute.String("error", ErrorTypeUnmarshalFunctionEvent),
				))
			}

			log.Printf("[ERROR] Failed to unmarshal function worker event: %v", err)
			return
		}

		if c.actionCounter != nil {
			c.actionCounter.Add(ctx, 1, metric.WithAttributes(
				attribute.String("action", string(op.Action)),
			))
		}

		// Intercept Cancel action to process immediately, bypassing the queue
		if op.Action == api.ActionCancel {
			log.Printf("📥 Received CANCEL command for function operation %s (Bypassing queue)", op.Payload.QueuedOperationID)

			success := c.unitQueues.CancelOperation(op.Payload.QueuedOperationID)

			// Send acknowledgment. A successful cancel sends Canceled (not Completed)
			// so callers can distinguish "we aborted the work" from "the work finished".
			startedAt := time.Now()
			terminatedAt := startedAt
			status := api.ActionStatusCanceled
			message := "Operation cancelled"
			if !success {
				status = api.ActionStatusFailed
				message = "Operation not found or already completed"
			}

			result := &api.ActionResult{
				UnitID:            op.Payload.InvocationRequest.UnitID,
				SpaceID:           op.Payload.InvocationRequest.SpaceID,
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
			c.sendResult(result)
			return
		}

		log.Printf("[INFO] Queueing function worker event: Unit=%s, Action=%s", op.Payload.InvocationRequest.UnitID.String(), op.Action)
		// Queue the function worker event for async processing
		c.unitQueues.QueueFunctionEvent(ctx, op, func(event api.FunctionWorkerEventRequest) {
			// Track this operation
			c.operationsWg.Add(1)
			defer c.operationsWg.Done()

			// Create cancellable context for this operation
			// WithoutCancel first to survive connection drops, then WithCancel for explicit cancellation
			operationCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))

			// Register the cancel function so operation can be canceled later
			operationID := event.Payload.QueuedOperationID
			c.unitQueues.RegisterCancelFunc(operationID, cancel)
			defer c.unitQueues.UnregisterCancelFunc(operationID)

			var workerContext = &defaultFunctionWorkerContext{
				ctx:       operationCtx,
				serverURL: c.serverURL,
				workerID:  c.workerID,
			}
			err := c.processFunctionCommand(workerContext, event)
			if err != nil {
				if errors.Is(err, context.Canceled) {
					log.Printf("[INFO] Operation %s was canceled for Unit=%s, Action=%s", operationID, event.Payload.InvocationRequest.UnitID.String(), event.Action)
				} else {
					log.Printf("[ERROR] Function command processing failed for Unit=%s, Action=%s: %v", event.Payload.InvocationRequest.UnitID.String(), event.Action, err)

					if c.errorCounter != nil {
						c.errorCounter.Add(ctx, 1, metric.WithAttributes(
							attribute.String("action", string(event.Action)),
							attribute.String("unit", event.Payload.InvocationRequest.UnitID.String()),
							attribute.String("error", ErrorTypeFunctionCommand),
						))
					}
				}
			} else {
				log.Printf("[INFO] Function command completed successfully for Unit=%s, Action=%s", event.Payload.InvocationRequest.UnitID.String(), event.Action)
			}
		})
		return
	}
}

// sendResult ships an action result to the ConfigHub server with retry. The
// transport owns the wire details and the retry policy; see
// longPollTransport.SendResult.
func (c *workerClient) sendResult(result *api.ActionResult) error {
	return c.longPoll.SendResult(result)
}

// sendResultOnce ships an action result without retry. Used by the status
// queue, which handles its own infinite retry.
func (c *workerClient) sendResultOnce(result *api.ActionResult) error {
	return c.longPoll.SendResultOnce(result)
}

// WaitForPendingOperations blocks until all in-flight operations are complete
func (c *workerClient) WaitForPendingOperations() {
	c.eventStateMu.Lock()
	c.serving = false
	c.eventStateMu.Unlock()

	// Long-poll releases its claim eagerly, before draining, so the server
	// marks the worker Disconnected without waiting on a long-running watcher.
	// SSE relies on stream-close to do the equivalent, so this is a no-op there
	// (longPoll is nil). See longPollTransport.BeginShutdown.
	if c.longPoll != nil {
		c.longPoll.BeginShutdown()
	}

	log.Printf("[INFO] Waiting for pending operations to complete...")
	c.operationsWg.Wait()
	log.Printf("[INFO] All operations completed")
}

// LastEventHandledAt returns the time the most recent event was handled by
// handleEventWithLogging. Returns the zero time if no event has been handled yet.
func (c *workerClient) LastEventHandledAt() time.Time {
	c.eventStateMu.RLock()
	defer c.eventStateMu.RUnlock()
	return c.lastEventHandledAt
}

// IsServing reports whether the worker is currently inside the event-stream
// read loop. It is set to true just before the loop begins and false when
// WaitForPendingOperations is called during shutdown.
func (c *workerClient) IsServing() bool {
	c.eventStateMu.RLock()
	defer c.eventStateMu.RUnlock()
	return c.serving
}
