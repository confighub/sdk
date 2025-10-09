// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package lib

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/alitto/pond"
	"github.com/confighub/sdk/bridge-worker/api"
	"github.com/google/uuid"
)

// WatcherManager combines watcher lifecycle management with worker pool execution.
// It ensures only one watcher is active per unit at any time, with newer watchers
// canceling and replacing older ones. It also manages the goroutine pool for watchers.
type WatcherManager struct {
	mu             sync.RWMutex
	activeWatchers map[uuid.UUID]context.CancelFunc
	pool           *pond.WorkerPool
}

// NewWatcherManager creates a new WatcherManager with the specified pool size
func NewWatcherManager(maxWorkers, maxQueueSize int) *WatcherManager {
	return &WatcherManager{
		activeWatchers: make(map[uuid.UUID]context.CancelFunc),
		pool:           pond.New(maxWorkers, maxQueueSize),
	}
}

// SubmitWatcher submits a watcher task for a unit, canceling any existing watcher.
// The task will be executed in the worker pool with proper lifecycle management.
func (m *WatcherManager) SubmitWatcher(ctx context.Context, unitID uuid.UUID, watchType api.ActionType, task func(context.Context)) {
	// Register and get cancellable context
	watcherCtx, wasReplaced := m.registerWatcher(ctx, unitID, watchType)

	// Submit to pool with automatic unregistration
	m.pool.Submit(func() {
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

// registerWatcher registers a new watcher for a unit, canceling any existing one.
// Returns a context that should be used for the watcher goroutine and whether an existing watcher was replaced.
func (m *WatcherManager) registerWatcher(ctx context.Context, unitID uuid.UUID, watchType api.ActionType) (context.Context, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Cancel existing watcher if present
	wasReplaced := false
	if cancel, exists := m.activeWatchers[unitID]; exists {
		log.Printf("🔄 CANCELLING existing %s watcher for unit %s", watchType, unitID)
		cancel()
		wasReplaced = true
		// Give a moment for the cancellation to propagate
		time.Sleep(10 * time.Millisecond)
	}

	// Create new context for this watcher
	watcherCtx, cancel := context.WithCancel(ctx)
	m.activeWatchers[unitID] = cancel

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

	if cancel, exists := m.activeWatchers[unitID]; exists {
		cancel() // Ensure it's canceled
		delete(m.activeWatchers, unitID)
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

	for unitID, cancel := range m.activeWatchers {
		log.Printf("🛑 Canceling watcher for unit %s", unitID)
		cancel()
	}
	m.activeWatchers = make(map[uuid.UUID]context.CancelFunc)
}

// StopAndWait stops the worker pool and waits for all workers to finish
func (m *WatcherManager) StopAndWait() {
	m.CancelAll()
	m.pool.StopAndWait()
}

// IsWatcherActive checks if a watcher is currently active for a unit
func (m *WatcherManager) IsWatcherActive(unitID uuid.UUID) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	_, exists := m.activeWatchers[unitID]
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
