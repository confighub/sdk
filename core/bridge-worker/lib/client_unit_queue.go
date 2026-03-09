// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package lib

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/cenkalti/backoff/v5"
	"github.com/confighub/sdk/bridge-worker/api"
	"github.com/google/uuid"
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
	bridgeQueues      map[string]*unitQueue
	functionQueues    map[string]*unitQueue
	statusQueues      map[string]*statusQueue // Per-unit queues for async status sending with infinite retry
	operationCancels  sync.Map                // Key: uuid.UUID (QueuedOperationID), Value: context.CancelFunc
	runningOperations sync.Map                // Key: string (unitID:category), Value: *runningOperation
	mu                sync.RWMutex
	wg                sync.WaitGroup
	cleanupCtx        context.Context
	cleanupCancel     context.CancelFunc
	errorChannel      chan error
}

// runningOperation tracks the currently running operation for a unit/category
// Used for automatic override detection (Apply overrides Apply, Destroy overrides Destroy)
type runningOperation struct {
	operationID uuid.UUID
	unitID      uuid.UUID
	spaceID     uuid.UUID
	action      api.ActionType
	startedAt   time.Time
	revisionNum int64

	// Lifecycle state tracking using existing API status types.
	// This prevents duplicate terminal status messages by ensuring only one
	// final status can be sent per operation.
	//
	// Status mapping for state machine:
	//   - api.ActionStatusNone      → Running (not yet terminal)
	//   - api.ActionStatusAborted   → Overridden by newer operation
	//   - api.ActionStatusCanceled  → Explicitly cancelled by user
	//   - api.ActionStatusCompleted → Successfully completed
	//   - api.ActionStatusFailed    → Failed with error
	//
	// Once transitioned to a terminal status (Aborted/Canceled/Completed/Failed),
	// no further status transitions are allowed.
	status   api.ActionStatusType
	statusMu sync.RWMutex // Protects status transitions
}

// TryTransitionToTerminal attempts atomic transition from None (running) to a terminal status.
//
// Valid transitions:
//   - ActionStatusNone → ActionStatusAborted   (operation overridden by newer one)
//   - ActionStatusNone → ActionStatusCanceled  (operation cancelled by user)
//   - ActionStatusNone → ActionStatusCompleted (operation completed successfully)
//   - ActionStatusNone → ActionStatusFailed    (operation failed with error)
//
// Returns true if the transition succeeded, false if the operation is already in a terminal state.
// This ensures only one terminal status can be sent per operation, preventing duplicates.
func (op *runningOperation) TryTransitionToTerminal(newStatus api.ActionStatusType) bool {
	op.statusMu.Lock()
	defer op.statusMu.Unlock()

	// Can only transition from None (running) to terminal statuses
	if op.status != api.ActionStatusNone {
		return false // Already terminal
	}

	// Validate terminal status
	if newStatus != api.ActionStatusAborted &&
		newStatus != api.ActionStatusCanceled &&
		newStatus != api.ActionStatusCompleted &&
		newStatus != api.ActionStatusFailed {
		return false // Invalid status
	}

	op.status = newStatus
	return true
}

// GetStatus returns current status (thread-safe)
func (op *runningOperation) GetStatus() api.ActionStatusType {
	op.statusMu.RLock()
	defer op.statusMu.RUnlock()
	return op.status
}

// IsTerminal returns whether operation is in terminal status
func (op *runningOperation) IsTerminal() bool {
	op.statusMu.RLock()
	defer op.statusMu.RUnlock()
	return op.status != api.ActionStatusNone
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

// statusQueue handles async status sending with infinite retry per unit
type statusQueue struct {
	unitID   string
	events   chan *statusEvent
	ctx      context.Context
	cancel   context.CancelFunc
	lastUsed time.Time
}

type statusEvent struct {
	result *api.ActionResult
	sendFn func(*api.ActionResult) error
}

// NewUnitQueueManager creates a new UnitQueueManager instance
func NewUnitQueueManager() *UnitQueueManager {
	return &UnitQueueManager{
		bridgeQueues:   make(map[string]*unitQueue),
		functionQueues: make(map[string]*unitQueue),
		statusQueues:   make(map[string]*statusQueue),
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

	// Cancel all tracked operations before shutdown
	u.operationCancels.Range(func(key, value interface{}) bool {
		operationID := key.(uuid.UUID)
		cancel := value.(context.CancelFunc)
		log.Printf("Cancelling operation %s during shutdown", operationID)
		cancel()
		return true
	})

	// First, cancel all queue contexts while holding the lock
	u.mu.Lock()
	for _, q := range u.bridgeQueues {
		q.cancel()
	}
	for _, q := range u.functionQueues {
		q.cancel()
	}
	for _, q := range u.statusQueues {
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
	for _, q := range u.statusQueues {
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

// =============================================================================
// Status Queue Operations (Async with Infinite Retry)
// =============================================================================
// These methods support async status sending with infinite retry per unit.
// Status updates are queued and sent in FIFO order, retrying forever until
// successful or the queue is stopped (on shutdown).

// QueueStatusEvent queues a status update for async delivery with infinite retry.
// Returns immediately (non-blocking). Status is delivered in FIFO order per unit.
func (u *UnitQueueManager) QueueStatusEvent(
	unitID string,
	result *api.ActionResult,
	sendFn func(*api.ActionResult) error,
) {
	q := u.getOrCreateStatusQueue(unitID)

	// Non-blocking send to buffered channel
	select {
	case q.events <- &statusEvent{
		result: result,
		sendFn: sendFn,
	}:
		q.lastUsed = time.Now()
	default:
		// Channel full - this should be rare with large buffer
		log.Printf("[WARN] Status queue full for unit %s, blocking until space available", unitID)
		q.events <- &statusEvent{
			result: result,
			sendFn: sendFn,
		}
		q.lastUsed = time.Now()
	}
}

func (u *UnitQueueManager) getOrCreateStatusQueue(unitID string) *statusQueue {
	u.mu.RLock()
	q, exists := u.statusQueues[unitID]
	u.mu.RUnlock()

	if exists {
		return q
	}

	u.mu.Lock()
	defer u.mu.Unlock()

	// Double-check after acquiring write lock
	q, exists = u.statusQueues[unitID]
	if exists {
		return q
	}

	// Create new status queue with large buffer
	queueCtx, cancel := context.WithCancel(u.cleanupCtx)
	q = &statusQueue{
		unitID:   unitID,
		events:   make(chan *statusEvent, 1000), // Large buffer for status events
		ctx:      queueCtx,
		cancel:   cancel,
		lastUsed: time.Now(),
	}
	u.statusQueues[unitID] = q

	// Start processor goroutine
	u.wg.Add(1)
	go u.processStatusQueue(q)

	log.Printf("Created status queue for unit %s", unitID)
	return q
}

func (u *UnitQueueManager) processStatusQueue(q *statusQueue) {
	defer u.wg.Done()

	log.Printf("Starting status queue processor for unit %s", q.unitID)

	for {
		select {
		case <-q.ctx.Done():
			// Drop all pending status on shutdown (per requirements)
			remaining := len(q.events)
			if remaining > 0 {
				log.Printf("Dropping %d pending status updates for unit %s on shutdown", remaining, q.unitID)
			}
			log.Printf("Stopping status queue processor for unit %s", q.unitID)
			return
		case event := <-q.events:
			u.sendWithInfiniteRetry(q.ctx, event)
		}
	}
}

func (u *UnitQueueManager) sendWithInfiniteRetry(ctx context.Context, event *statusEvent) {
	eb := &backoff.ExponentialBackOff{
		InitialInterval:     1 * time.Second,
		RandomizationFactor: 0.5,
		Multiplier:          2.0,
		MaxInterval:         30 * time.Second,
	}

	attempt := 0
	for {
		attempt++

		err := event.sendFn(event.result)
		if err == nil {
			if attempt > 1 {
				log.Printf("[INFO] Status sent successfully after %d attempts", attempt)
			}
			return
		}

		// Check for permanent errors (4xx except 409)
		if isPermanentError(err) {
			log.Printf("[ERROR] Permanent error sending status, dropping: %v", err)
			return
		}

		// Check if we should stop
		select {
		case <-ctx.Done():
			log.Printf("[WARN] Status queue stopped, dropping status after %d attempts", attempt)
			return
		default:
		}

		// Exponential backoff for retryable errors
		delay := eb.NextBackOff()
		log.Printf("[WARN] Status send failed (attempt #%d), retrying in %v: %v", attempt, delay, err)

		select {
		case <-time.After(delay):
			// Continue retry
		case <-ctx.Done():
			log.Printf("[WARN] Status queue stopped during backoff, dropping status")
			return
		}
	}
}

// isPermanentError checks if an error is permanent (should not retry)
func isPermanentError(err error) bool {
	errStr := err.Error()
	// 4xx errors except 409 and 429 are permanent
	return (strings.Contains(errStr, "status 4") &&
		!strings.Contains(errStr, "status 409") &&
		!strings.Contains(errStr, "status 429")) ||
		(strings.Contains(errStr, "status 5") &&
			!strings.Contains(errStr, "status 503"))
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
	var statusToCleanup []*statusQueue

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
	for unitID, q := range u.statusQueues {
		if now.Sub(q.lastUsed) > queueIdleTimeout {
			log.Printf("Marking idle status queue for cleanup: unit %s", unitID)
			statusToCleanup = append(statusToCleanup, q)
		}
	}
	u.mu.RUnlock()

	// Second pass: cancel contexts (no lock held)
	for _, q := range toCleanup {
		q.cancel()
	}
	for _, q := range statusToCleanup {
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

	for unitID, q := range u.statusQueues {
		if now.Sub(q.lastUsed) > queueIdleTimeout {
			log.Printf("Cleaning up idle status queue for unit %s", unitID)
			close(q.events)
			delete(u.statusQueues, unitID)
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

// =============================================================================
// Cancel Operations
// =============================================================================
// These methods support explicit cancellation via ActionCancel commands.
// The cancel function is registered when an operation starts and called when
// a Cancel command is received (bypassing the queue for immediate effect).

// RegisterCancelFunc stores a cancel function for an operation
// This allows the operation to be canceled later via CancelOperation
func (u *UnitQueueManager) RegisterCancelFunc(operationID uuid.UUID, cancel context.CancelFunc) {
	u.operationCancels.Store(operationID, cancel)
	log.Printf("Registered cancel function for operation %s", operationID)
}

// UnregisterCancelFunc removes a cancel function when operation completes
// This should be called via defer to ensure cleanup even if operation fails
func (u *UnitQueueManager) UnregisterCancelFunc(operationID uuid.UUID) {
	u.operationCancels.Delete(operationID)
	log.Printf("Unregistered cancel function for operation %s", operationID)
}

// CancelOperation cancels a running operation by its ID
// Returns true if operation was found and cancelled, false otherwise
func (u *UnitQueueManager) CancelOperation(operationID uuid.UUID) bool {
	if cancel, ok := u.operationCancels.Load(operationID); ok {
		cancelFunc := cancel.(context.CancelFunc)
		cancelFunc()
		log.Printf("Cancelled operation %s", operationID)
		return true
	}
	log.Printf("No active operation found for ID %s", operationID)
	return false
}

// GetRunningOperationByID looks up a running operation by its operation ID
// Returns the running operation info if found, nil otherwise
// This iterates over all running operations since they are keyed by unitID:category
func (u *UnitQueueManager) GetRunningOperationByID(operationID uuid.UUID) *runningOperation {
	var found *runningOperation
	u.runningOperations.Range(func(key, value interface{}) bool {
		op := value.(*runningOperation)
		if op.operationID == operationID {
			found = op
			return false // stop iteration
		}
		return true // continue iteration
	})
	return found
}

// =============================================================================
// Override Operations
// =============================================================================
// These methods support automatic override for same-type operations.
// When a new Apply/Destroy operation starts, it checks for and cancels any
// running same-type operation (Apply overrides Apply, Destroy overrides Destroy).
// Uses CancelOperation internally to trigger the actual cancellation.

// makeRunningOperationKey creates a key for tracking running operations per unit per category
// Returns empty string for non-overrideable actions
// Note: Apply and WatchForApply share "apply" category (same operation flow)
//
//	Destroy and WatchForDestroy share "destroy" category (same operation flow)
func makeRunningOperationKey(unitID uuid.UUID, action api.ActionType) string {
	switch action {
	case api.ActionApply, api.ActionDestroy:
		return unitID.String() + ":" + strings.ToLower(string(action))
	default:
		return ""
	}
}

// SetRunningOperation registers an operation as currently running for a unit/category
// This enables automatic override detection for same-type operations
func (u *UnitQueueManager) SetRunningOperation(unitID, spaceID, operationID uuid.UUID, action api.ActionType, revisionNum int64) {
	key := makeRunningOperationKey(unitID, action)
	if key == "" {
		return // Not an overrideable action
	}

	op := &runningOperation{
		operationID: operationID,
		unitID:      unitID,
		spaceID:     spaceID,
		action:      action,
		startedAt:   time.Now(),
		revisionNum: revisionNum,
		status:      api.ActionStatusNone, // Initialize to None (running/not terminal)
	}
	u.runningOperations.Store(key, op)
	log.Printf("Registered running %s operation %s for unit %s (revision %d)", action, operationID, unitID, revisionNum)
}

// ClearRunningOperation removes the running operation tracking for a unit/category
// Only removes if the operationID matches (prevents removing a replacement operation)
func (u *UnitQueueManager) ClearRunningOperation(unitID uuid.UUID, action api.ActionType, operationID uuid.UUID) {
	key := makeRunningOperationKey(unitID, action)
	if key == "" {
		return
	}

	// Only clear if this operation is still the current one
	if val, ok := u.runningOperations.Load(key); ok {
		running := val.(*runningOperation)
		if running.operationID == operationID {
			u.runningOperations.Delete(key)
			log.Printf("Cleared running %s operation %s for unit %s", action, operationID, unitID)
		}
	}
}

// GetAndCancelRunningOperation checks for and cancels a running same-type operation
// Returns the cancelled operation info and true if one was found and cancelled
// Returns nil and false if no conflicting operation was running
func (u *UnitQueueManager) GetAndCancelRunningOperation(unitID uuid.UUID, action api.ActionType) (*runningOperation, bool) {
	key := makeRunningOperationKey(unitID, action)
	if key == "" {
		return nil, false // Not an overrideable action
	}

	if val, ok := u.runningOperations.Load(key); ok {
		running := val.(*runningOperation)
		// Cancel the running operation
		if u.CancelOperation(running.operationID) {
			log.Printf("🔄 Overriding running %s operation %s for unit %s", running.action, running.operationID, unitID)
			return running, true
		}
	}
	return nil, false
}
