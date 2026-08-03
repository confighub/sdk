// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package lib

import (
	"context"
	"testing"
	"time"

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
