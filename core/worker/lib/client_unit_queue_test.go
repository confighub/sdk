// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package lib

import (
	"context"
	"testing"
	"time"

	"github.com/confighub/sdk/worker/api"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func TestUnitQueueManager_RegisterUnregisterCancelFunc(t *testing.T) {
	qm := NewUnitQueueManager()
	qm.Start(context.Background())
	defer qm.Stop()

	opID := uuid.New()
	_, cancel := context.WithCancel(context.Background())
	
	// Register
	qm.RegisterCancelFunc(opID, cancel)
	
	// Verify stored
	val, ok := qm.operationCancels.Load(opID)
	assert.True(t, ok)
	assert.NotNil(t, val)
	
	// Unregister
	qm.UnregisterCancelFunc(opID)
	
	// Verify removed
	val, ok = qm.operationCancels.Load(opID)
	assert.False(t, ok)
	assert.Nil(t, val)
	
	// Cleanup
	cancel()
}

func TestUnitQueueManager_CancelOperation(t *testing.T) {
	qm := NewUnitQueueManager()
	qm.Start(context.Background())
	defer qm.Stop()

	opID := uuid.New()
	ctx, cancel := context.WithCancel(context.Background())
	
	// Register
	qm.RegisterCancelFunc(opID, cancel)
	
	// Verify context is alive
	select {
	case <-ctx.Done():
		t.Fatal("Context should be alive")
	default:
	}
	
	// Cancel
	success := qm.CancelOperation(opID)
	assert.True(t, success)
	
	// Verify context is cancelled
	select {
	case <-ctx.Done():
		// OK
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Context should be cancelled")
	}
	
	// Cancel should NOT remove the func immediately (Unregister does that)
	// But the implementation of CancelOperation doesn't say it removes it.
	// Let's check the implementation again.
	// "CancelOperation cancels a running operation by its ID"
	// It does NOT remove it. The operation handles removing via defer Unregister.
	
	val, ok := qm.operationCancels.Load(opID)
	assert.True(t, ok)
	assert.NotNil(t, val)
}

func TestUnitQueueManager_CancelNonExistentOperation(t *testing.T) {
	qm := NewUnitQueueManager()
	qm.Start(context.Background())
	defer qm.Stop()

	opID := uuid.New()
	
	// Cancel unknown op
	success := qm.CancelOperation(opID)
	assert.False(t, success)
}

func TestUnitQueueManager_StopCancelsAllOperations(t *testing.T) {
	qm := NewUnitQueueManager()
	qm.Start(context.Background())

	opID1 := uuid.New()
	ctx1, cancel1 := context.WithCancel(context.Background())
	qm.RegisterCancelFunc(opID1, cancel1)

	opID2 := uuid.New()
	ctx2, cancel2 := context.WithCancel(context.Background())
	qm.RegisterCancelFunc(opID2, cancel2)
	
	// Stop manager
	qm.Stop()
	
	// Verify all contexts cancelled
	select {
	case <-ctx1.Done():
		// OK
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Context 1 should be cancelled")
	}

	select {
	case <-ctx2.Done():
		// OK
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Context 2 should be cancelled")
	}
}

// Tests for running operation tracking and override functionality

func TestUnitQueueManager_SetAndClearRunningOperation(t *testing.T) {
	qm := NewUnitQueueManager()
	qm.Start(context.Background())
	defer qm.Stop()

	unitID := uuid.New()
	spaceID := uuid.New()
	opID := uuid.New()

	// Set running operation
	qm.SetRunningOperation(unitID, spaceID, opID, api.ActionApply, 1)

	// Verify it's stored
	key := makeRunningOperationKey(unitID, api.ActionApply)
	val, ok := qm.runningOperations.Load(key)
	assert.True(t, ok)
	assert.NotNil(t, val)

	running := val.(*runningOperation)
	assert.Equal(t, opID, running.operationID)
	assert.Equal(t, unitID, running.unitID)
	assert.Equal(t, spaceID, running.spaceID)
	assert.Equal(t, api.ActionApply, running.action)

	// Clear running operation
	qm.ClearRunningOperation(unitID, api.ActionApply, opID)

	// Verify it's removed
	_, ok = qm.runningOperations.Load(key)
	assert.False(t, ok)
}

func TestUnitQueueManager_ClearRunningOperationOnlyMatchingID(t *testing.T) {
	qm := NewUnitQueueManager()
	qm.Start(context.Background())
	defer qm.Stop()

	unitID := uuid.New()
	spaceID := uuid.New()
	opID1 := uuid.New()
	opID2 := uuid.New()

	// Set running operation with opID1
	qm.SetRunningOperation(unitID, spaceID, opID1, api.ActionApply, 1)

	// Try to clear with different opID - should NOT remove
	qm.ClearRunningOperation(unitID, api.ActionApply, opID2)

	// Verify it's still there
	key := makeRunningOperationKey(unitID, api.ActionApply)
	val, ok := qm.runningOperations.Load(key)
	assert.True(t, ok)
	running := val.(*runningOperation)
	assert.Equal(t, opID1, running.operationID)

	// Clear with correct opID - should remove
	qm.ClearRunningOperation(unitID, api.ActionApply, opID1)
	_, ok = qm.runningOperations.Load(key)
	assert.False(t, ok)
}

func TestUnitQueueManager_OverrideApplyWithApply(t *testing.T) {
	qm := NewUnitQueueManager()
	qm.Start(context.Background())
	defer qm.Stop()

	unitID := uuid.New()
	spaceID := uuid.New()
	opID1 := uuid.New()
	opID2 := uuid.New()

	// Create cancellable context for first operation
	ctx1, cancel1 := context.WithCancel(context.Background())
	qm.RegisterCancelFunc(opID1, cancel1)
	qm.SetRunningOperation(unitID, spaceID, opID1, api.ActionApply, 1)

	// Verify context is alive
	select {
	case <-ctx1.Done():
		t.Fatal("Context 1 should be alive")
	default:
	}

	// Second Apply operation should override the first
	overridden, ok := qm.GetAndCancelRunningOperation(unitID, api.ActionApply)
	assert.True(t, ok)
	assert.NotNil(t, overridden)
	assert.Equal(t, opID1, overridden.operationID)
	assert.Equal(t, api.ActionApply, overridden.action)

	// Verify first context is now cancelled
	select {
	case <-ctx1.Done():
		// OK - context was cancelled
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Context 1 should be cancelled after override")
	}

	// Register the new operation
	_, cancel2 := context.WithCancel(context.Background())
	qm.RegisterCancelFunc(opID2, cancel2)
	qm.SetRunningOperation(unitID, spaceID, opID2, api.ActionApply, 1)

	// Verify new operation is now tracked
	key := makeRunningOperationKey(unitID, api.ActionApply)
	val, _ := qm.runningOperations.Load(key)
	running := val.(*runningOperation)
	assert.Equal(t, opID2, running.operationID)
}

func TestUnitQueueManager_OverrideDestroyWithDestroy(t *testing.T) {
	qm := NewUnitQueueManager()
	qm.Start(context.Background())
	defer qm.Stop()

	unitID := uuid.New()
	spaceID := uuid.New()
	opID1 := uuid.New()

	// Create cancellable context for first operation
	ctx1, cancel1 := context.WithCancel(context.Background())
	qm.RegisterCancelFunc(opID1, cancel1)
	qm.SetRunningOperation(unitID, spaceID, opID1, api.ActionDestroy, 1)

	// Second Destroy operation should override the first
	overridden, ok := qm.GetAndCancelRunningOperation(unitID, api.ActionDestroy)
	assert.True(t, ok)
	assert.NotNil(t, overridden)
	assert.Equal(t, opID1, overridden.operationID)
	assert.Equal(t, api.ActionDestroy, overridden.action)

	// Verify context is cancelled
	select {
	case <-ctx1.Done():
		// OK
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Context should be cancelled after override")
	}
}

func TestUnitQueueManager_NoOverrideApplyWithDestroy(t *testing.T) {
	qm := NewUnitQueueManager()
	qm.Start(context.Background())
	defer qm.Stop()

	unitID := uuid.New()
	spaceID := uuid.New()
	opID1 := uuid.New()

	// Create cancellable context for Apply operation
	ctx1, cancel1 := context.WithCancel(context.Background())
	qm.RegisterCancelFunc(opID1, cancel1)
	qm.SetRunningOperation(unitID, spaceID, opID1, api.ActionApply, 1)

	// Destroy should NOT override Apply
	overridden, ok := qm.GetAndCancelRunningOperation(unitID, api.ActionDestroy)
	assert.False(t, ok)
	assert.Nil(t, overridden)

	// Verify Apply context is still alive
	select {
	case <-ctx1.Done():
		t.Fatal("Apply context should still be alive")
	default:
		// OK
	}

	// Cleanup
	cancel1()
}

func TestUnitQueueManager_NoOverrideDestroyWithApply(t *testing.T) {
	qm := NewUnitQueueManager()
	qm.Start(context.Background())
	defer qm.Stop()

	unitID := uuid.New()
	spaceID := uuid.New()
	opID1 := uuid.New()

	// Create cancellable context for Destroy operation
	ctx1, cancel1 := context.WithCancel(context.Background())
	qm.RegisterCancelFunc(opID1, cancel1)
	qm.SetRunningOperation(unitID, spaceID, opID1, api.ActionDestroy, 1)

	// Apply should NOT override Destroy
	overridden, ok := qm.GetAndCancelRunningOperation(unitID, api.ActionApply)
	assert.False(t, ok)
	assert.Nil(t, overridden)

	// Verify Destroy context is still alive
	select {
	case <-ctx1.Done():
		t.Fatal("Destroy context should still be alive")
	default:
		// OK
	}

	// Cleanup
	cancel1()
}

func TestUnitQueueManager_NonOverrideableActions(t *testing.T) {
	qm := NewUnitQueueManager()
	qm.Start(context.Background())
	defer qm.Stop()

	unitID := uuid.New()
	spaceID := uuid.New()
	opID := uuid.New()

	// Set running operation for non-overrideable action (Refresh)
	qm.SetRunningOperation(unitID, spaceID, opID, api.ActionRefresh, 1)

	// Verify nothing is stored (Refresh is not overrideable)
	key := makeRunningOperationKey(unitID, api.ActionRefresh)
	assert.Equal(t, "", key) // Key should be empty for non-overrideable actions

	// GetAndCancel should return false for non-overrideable actions
	overridden, ok := qm.GetAndCancelRunningOperation(unitID, api.ActionRefresh)
	assert.False(t, ok)
	assert.Nil(t, overridden)
}
