// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package lib

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/confighub/sdk/bridge-worker/api"
)

// QueueType represents the type of event queue
type QueueType string

const (
	BridgeQueueType   QueueType = "bridge"
	FunctionQueueType QueueType = "function"
)

const (
	queueIdleTimeout   = 5 * time.Minute
	cleanupInterval    = 1 * time.Minute
	workItemTimeout    = 60 * time.Minute // Allow very long-running operations
	errorChannelBuffer = 100              // Buffer for error channel
)

// UnitQueueManager manages queues for different units to ensure serialized operations
type UnitQueueManager struct {
	bridgeQueues   map[string]*unitQueue
	functionQueues map[string]*unitQueue
	mu             sync.RWMutex
	wg             sync.WaitGroup
	cleanupCtx     context.Context
	cleanupCancel  context.CancelFunc
	errorChannel   chan error
}

type unitQueue struct {
	unitID   string
	events   chan queuedEvent
	ctx      context.Context
	cancel   context.CancelFunc
	lastUsed time.Time
}

type queuedEvent struct {
	event   interface{}
	handler func()
	ctx     context.Context
}

// NewUnitQueueManager creates a new UnitQueueManager instance
func NewUnitQueueManager() *UnitQueueManager {
	return &UnitQueueManager{
		bridgeQueues:   make(map[string]*unitQueue),
		functionQueues: make(map[string]*unitQueue),
		errorChannel:   make(chan error, errorChannelBuffer),
	}
}

// Start initializes the queue manager
func (u *UnitQueueManager) Start(ctx context.Context) {
	u.cleanupCtx, u.cleanupCancel = context.WithCancel(ctx)
	u.wg.Add(1)
	go u.cleanupIdleQueues()
}

// Stop gracefully shuts down the queue manager and all its queues
func (u *UnitQueueManager) Stop() {
	// Cancel cleanup goroutine first
	if u.cleanupCancel != nil {
		u.cleanupCancel()
	}

	// First, cancel all queue contexts while holding the lock
	u.mu.Lock()
	for _, q := range u.bridgeQueues {
		q.cancel()
	}
	for _, q := range u.functionQueues {
		q.cancel()
	}
	u.mu.Unlock()

	// Wait for all goroutines to finish (without holding the lock)
	u.wg.Wait()

	// Now safely close all channels
	u.mu.Lock()
	defer u.mu.Unlock()
	for _, q := range u.bridgeQueues {
		close(q.events)
	}
	for _, q := range u.functionQueues {
		close(q.events)
	}

	// Close error channel
	close(u.errorChannel)
}

func (u *UnitQueueManager) getOrCreateQueue(unitID string, queueType QueueType, ctx context.Context) *unitQueue {
	// Helper function to get the correct queue map
	getQueuesMap := func() map[string]*unitQueue {
		switch queueType {
		case BridgeQueueType:
			return u.bridgeQueues
		case FunctionQueueType:
			return u.functionQueues
		default:
			log.Printf("Unknown queue type: %s, defaulting to bridge", queueType)
			return u.bridgeQueues
		}
	}

	u.mu.RLock()
	queues := getQueuesMap()
	q, exists := queues[unitID]
	u.mu.RUnlock()

	if exists {
		return q
	}

	u.mu.Lock()
	defer u.mu.Unlock()

	// Double-check after acquiring write lock with fresh reference
	queues = getQueuesMap()
	q, exists = queues[unitID]
	if exists {
		return q
	}

	// Create new queue
	queueCtx, cancel := context.WithCancel(ctx)
	q = &unitQueue{
		unitID:   unitID,
		events:   make(chan queuedEvent, 100), // Buffered channel to avoid blocking
		ctx:      queueCtx,
		cancel:   cancel,
		lastUsed: time.Now(),
	}
	queues[unitID] = q

	// Start queue processor
	u.wg.Add(1)
	go u.processQueue(q, queueType)

	return q
}

func (u *UnitQueueManager) processQueue(q *unitQueue, queueType QueueType) {
	defer u.wg.Done()

	log.Printf("Starting %s queue processor for unit %s", queueType, q.unitID)

	for {
		select {
		case <-q.ctx.Done():
			log.Printf("Stopping %s queue processor for unit %s", queueType, q.unitID)
			return
		case event, ok := <-q.events:
			if !ok {
				log.Printf("%s queue closed for unit %s", queueType, q.unitID)
				return
			}

			// Update last used time before processing
			q.lastUsed = time.Now()

			// Process event with timeout protection to prevent deadlocks
			u.processEventWithTimeout(event, queueType, q.unitID)
		}
	}
}

// QueueBridgeEvent enqueues a bridge worker event to be processed serially for its unit
func (u *UnitQueueManager) QueueBridgeEvent(ctx context.Context, event api.BridgeWorkerEventRequest, handler func(api.BridgeWorkerEventRequest)) {
	unitID := event.Payload.UnitID.String()
	q := u.getOrCreateQueue(unitID, BridgeQueueType, ctx)

	select {
	case <-ctx.Done():
		log.Printf("Context cancelled, not queuing bridge event for unit %s", unitID)
		return
	case q.events <- queuedEvent{
		event: event,
		ctx:   ctx,
		handler: func() {
			handler(event)
		},
	}:
		log.Printf("Queued bridge event for unit %s, action %s", unitID, event.Action)
	default:
		log.Printf("Bridge queue full for unit %s, dropping bridge event", unitID)
	}
}

// QueueFunctionEvent enqueues a function worker event to be processed serially for its unit
func (u *UnitQueueManager) QueueFunctionEvent(ctx context.Context, event api.FunctionWorkerEventRequest, handler func(api.FunctionWorkerEventRequest)) {
	unitID := event.Payload.InvocationRequest.UnitID.String()
	q := u.getOrCreateQueue(unitID, FunctionQueueType, ctx)

	select {
	case <-ctx.Done():
		log.Printf("Context cancelled, not queuing function event for unit %s", unitID)
		return
	case q.events <- queuedEvent{
		event: event,
		ctx:   ctx,
		handler: func() {
			handler(event)
		},
	}:
		log.Printf("Queued function event for unit %s, action %s", unitID, event.Action)
	default:
		log.Printf("Function queue full for unit %s, dropping function event", unitID)
	}
}

// cleanupIdleQueues periodically removes idle queues to prevent resource leaks
func (u *UnitQueueManager) cleanupIdleQueues() {
	defer u.wg.Done()

	ticker := time.NewTicker(cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-u.cleanupCtx.Done():
			log.Printf("Cleanup goroutine stopping")
			return
		case <-ticker.C:
			u.performCleanup()
		}
	}
}

// performCleanup removes queues that have been idle for too long
func (u *UnitQueueManager) performCleanup() {
	now := time.Now()
	var toCleanup []*unitQueue

	// First pass: identify queues to cleanup (with read lock)
	u.mu.RLock()
	for unitID, q := range u.bridgeQueues {
		if now.Sub(q.lastUsed) > queueIdleTimeout {
			log.Printf("Marking idle bridge queue for cleanup: unit %s", unitID)
			toCleanup = append(toCleanup, q)
		}
	}
	for unitID, q := range u.functionQueues {
		if now.Sub(q.lastUsed) > queueIdleTimeout {
			log.Printf("Marking idle function queue for cleanup: unit %s", unitID)
			toCleanup = append(toCleanup, q)
		}
	}
	u.mu.RUnlock()

	// Second pass: cancel contexts (no lock held)
	for _, q := range toCleanup {
		q.cancel()
	}

	// Third pass: remove from maps and close channels (with write lock)
	u.mu.Lock()
	defer u.mu.Unlock()

	for unitID, q := range u.bridgeQueues {
		if now.Sub(q.lastUsed) > queueIdleTimeout {
			log.Printf("Cleaning up idle bridge queue for unit %s", unitID)
			close(q.events)
			delete(u.bridgeQueues, unitID)
		}
	}

	for unitID, q := range u.functionQueues {
		if now.Sub(q.lastUsed) > queueIdleTimeout {
			log.Printf("Cleaning up idle function queue for unit %s", unitID)
			close(q.events)
			delete(u.functionQueues, unitID)
		}
	}
}

// processEventWithTimeout executes an event handler with timeout protection
func (u *UnitQueueManager) processEventWithTimeout(event queuedEvent, queueType QueueType, unitID string) {
	done := make(chan error)

	// Run the handler in a separate goroutine
	go func() {
		defer func() {
			if r := recover(); r != nil {
				panicErr := fmt.Errorf("panic in %s handler for unit %s: %v", queueType, unitID, r)
				log.Printf("Panic in %s event handler for unit %s: %v", queueType, unitID, r)
				u.reportError(panicErr)
				done <- panicErr
				return
			}
			close(done)
		}()

		log.Printf("Processing %s event for unit %s", queueType, unitID)
		event.handler()
	}()

	// Wait for completion with timeout or context cancellation
	select {
	case err := <-done:
		if err != nil {
			// Handler panicked
			log.Printf("Handler failed for %s event (unit %s): %v", queueType, unitID, err)
		} else {
			// Event completed successfully
			log.Printf("Completed %s event for unit %s", queueType, unitID)
		}
	case <-time.After(workItemTimeout):
		// Event timed out
		timeoutErr := fmt.Errorf("%s handler for unit %s timed out after %v", queueType, unitID, workItemTimeout)
		log.Printf("WARNING: %s event for unit %s timed out after %v", queueType, unitID, workItemTimeout)
		u.reportError(timeoutErr)
	case <-event.ctx.Done():
		// Context was cancelled
		cancelErr := fmt.Errorf("%s handler for unit %s cancelled: %v", queueType, unitID, event.ctx.Err())
		log.Printf("Context cancelled for %s event for unit %s", queueType, unitID)
		u.reportError(cancelErr)
	}
}

// ErrorChannel returns a read-only channel for receiving queue errors
// Usage example:
//
//	go func() {
//	  for err := range manager.ErrorChannel() {
//	    log.Printf("Queue error: %v", err)
//	  }
//	}()
func (u *UnitQueueManager) ErrorChannel() <-chan error {
	return u.errorChannel
}

// reportError sends an error to the error channel (non-blocking)
func (u *UnitQueueManager) reportError(err error) {
	select {
	case u.errorChannel <- err:
		// Error reported successfully
	default:
		// Error channel is full, log the error instead
		log.Printf("[WARNING] Error channel full, dropping error report: %v", err)
	}
}
