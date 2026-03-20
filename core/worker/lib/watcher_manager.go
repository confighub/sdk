// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package lib

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/alitto/pond"
	"github.com/confighub/sdk/core/worker/api"
	"github.com/google/uuid"
)

// WatcherManager combines watcher lifecycle management with worker pool execution.
// It ensures only one watcher is active per unit per watch type at any time,
// with newer watchers canceling and replacing older ones of the same type.
// This means Apply watchers only replace Apply watchers, and Destroy watchers
// only replace Destroy watchers. It also manages the goroutine pool for watchers.
type WatcherManager struct {
	mu             sync.RWMutex
	activeWatchers map[string]context.CancelFunc // Key: "unitID:actionType" (e.g., "uuid:Apply")
	pool           *pond.WorkerPool
}

// makeWatcherKey creates a composite key for tracking watchers per unit per type
func makeWatcherKey(unitID uuid.UUID, watchType api.ActionType) string {
	return fmt.Sprintf("%s:%s", unitID.String(), watchType)
}

// NewWatcherManager creates a new WatcherManager with the specified pool size
func NewWatcherManager(maxWorkers, maxQueueSize int) *WatcherManager {
	return &WatcherManager{
		activeWatchers: make(map[string]context.CancelFunc),
		pool:           pond.New(maxWorkers, maxQueueSize),
	}
}

// SubmitWatcher submits a watcher task for a unit, canceling any existing watcher.
// The task will be executed in the worker pool with proper lifecycle management.
// This function returns immediately (async). Use SubmitWatcherAndWait for sync behavior.
func (m *WatcherManager) SubmitWatcher(ctx context.Context, unitID uuid.UUID, watchType api.ActionType, task func(context.Context)) {
	m.submitWatcherInternal(ctx, unitID, watchType, task, nil)
}

// SubmitWatcherAndWait submits a watcher task and waits for it to complete.
// This keeps the caller blocked until the watcher finishes, which is important
// for maintaining running operation registration during override detection.
func (m *WatcherManager) SubmitWatcherAndWait(ctx context.Context, unitID uuid.UUID, watchType api.ActionType, task func(context.Context)) {
	done := make(chan struct{})
	m.submitWatcherInternal(ctx, unitID, watchType, task, done)
	<-done
}

// submitWatcherInternal is the common implementation for both sync and async watcher submission.
func (m *WatcherManager) submitWatcherInternal(ctx context.Context, unitID uuid.UUID, watchType api.ActionType, task func(context.Context), done chan struct{}) {
	// Register and get cancellable context
	watcherCtx, wasReplaced := m.registerWatcher(ctx, unitID, watchType)

	// Submit to pool with automatic unregistration
	m.pool.Submit(func() {
		if done != nil {
			defer close(done)
		}
		startTime := time.Now()
		log.Printf("🚀 Starting %s watcher for unit %s (replaced_existing=%v)", watchType, unitID, wasReplaced)

		defer func() {
			m.unregisterWatcher(unitID, watchType)
			duration := time.Since(startTime)

			// Check how the watcher ended
			var endReason string
			select {
			case <-watcherCtx.Done():
				endReason = "cancelled: " + watcherCtx.Err().Error()
			default:
				endReason = "completed normally"
			}
			log.Printf("⏱️ %s watcher for unit %s finished after %v (%s)",
				watchType, unitID, duration, endReason)
		}()

		// Check if context was canceled before starting
		select {
		case <-watcherCtx.Done():
			log.Printf("⚠️ %s watcher for unit %s canceled before execution (reason: %v)",
				watchType, unitID, watcherCtx.Err())
			return
		default:
		}

		// Execute the watcher task
		log.Printf("▶️ Executing %s watcher task for unit %s", watchType, unitID)
		task(watcherCtx)
	})
}

// registerWatcher registers a new watcher for a unit and watch type, canceling any existing one of the same type.
// Returns a context that should be used for the watcher goroutine and whether an existing watcher was replaced.
// Note: Apply watchers only replace Apply watchers, Destroy watchers only replace Destroy watchers.
func (m *WatcherManager) registerWatcher(ctx context.Context, unitID uuid.UUID, watchType api.ActionType) (context.Context, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := makeWatcherKey(unitID, watchType)

	// Cancel existing watcher of the same type if present
	wasReplaced := false
	if cancel, exists := m.activeWatchers[key]; exists {
		log.Printf("🔄 CANCELLING existing %s watcher for unit %s", watchType, unitID)
		cancel()
		wasReplaced = true
		// Give a moment for the cancellation to propagate
		time.Sleep(10 * time.Millisecond)
	}

	// Create new context for this watcher
	watcherCtx, cancel := context.WithCancel(ctx)
	m.activeWatchers[key] = cancel

	if wasReplaced {
		log.Printf("✅ Registered REPLACEMENT %s watcher for unit %s", watchType, unitID)
	} else {
		log.Printf("✅ Registered NEW %s watcher for unit %s", watchType, unitID)
	}
	return watcherCtx, wasReplaced
}

// unregisterWatcher removes a watcher from tracking when it completes
func (m *WatcherManager) unregisterWatcher(unitID uuid.UUID, watchType api.ActionType) {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := makeWatcherKey(unitID, watchType)

	if cancel, exists := m.activeWatchers[key]; exists {
		cancel() // Ensure it's canceled
		delete(m.activeWatchers, key)
		log.Printf("🔚 Unregistered %s watcher for unit %s (removed from tracking)", watchType, unitID)
	} else {
		log.Printf("⚠️ Attempted to unregister %s watcher for unit %s but it wasn't found (likely replaced)",
			watchType, unitID)
	}
}

// CancelAll cancels all active watchers (useful for shutdown)
func (m *WatcherManager) CancelAll() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for key, cancel := range m.activeWatchers {
		log.Printf("🛑 Canceling watcher %s", key)
		cancel()
	}
	m.activeWatchers = make(map[string]context.CancelFunc)
}

// StopAndWait stops the worker pool and waits for all workers to finish
func (m *WatcherManager) StopAndWait() {
	m.CancelAll()
	m.pool.StopAndWait()
}

// IsWatcherActive checks if a watcher of a specific type is currently active for a unit
func (m *WatcherManager) IsWatcherActive(unitID uuid.UUID, watchType api.ActionType) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	key := makeWatcherKey(unitID, watchType)
	_, exists := m.activeWatchers[key]
	return exists
}

// Stats returns statistics about the watcher manager
func (m *WatcherManager) Stats() (activeWatchers int, runningWorkers int, idleWorkers int, pendingTasks int) {
	m.mu.RLock()
	activeWatchers = len(m.activeWatchers)
	m.mu.RUnlock()

	runningWorkers = m.pool.RunningWorkers()
	idleWorkers = m.pool.IdleWorkers()
	pendingTasks = int(m.pool.WaitingTasks())

	return
}
