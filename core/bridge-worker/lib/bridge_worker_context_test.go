// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package lib

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/confighub/sdk/bridge-worker/api"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func TestDefaultWorkerContext_Context(t *testing.T) {
	// Create a mock context
	ctx := context.Background()

	// Create a worker context with the mock context
	workerCtx := &defaultBridgeWorkerContext{
		ctx:        ctx,
		serverURL:  "https://example.com",
		workerID:   "worker-123",
		sendResult: func(*api.ActionResult) error { return nil },
	}

	// Assert that the Context method returns the expected context
	assert.Equal(t, ctx, workerCtx.Context())
}

func TestDefaultWorkerContext_GetServerURL(t *testing.T) {
	// Create a worker context with a specific server URL
	serverURL := "https://example.com"
	workerCtx := &defaultBridgeWorkerContext{
		ctx:        context.Background(),
		serverURL:  serverURL,
		workerID:   "worker-123",
		sendResult: func(*api.ActionResult) error { return nil },
	}

	// Assert that the GetServerURL method returns the expected URL
	assert.Equal(t, serverURL, workerCtx.GetServerURL())
}

func TestDefaultWorkerContext_GetWorkerID(t *testing.T) {
	// Create a worker context with a specific worker ID
	workerID := "worker-123"
	workerCtx := &defaultBridgeWorkerContext{
		ctx:        context.Background(),
		serverURL:  "https://example.com",
		workerID:   workerID,
		sendResult: func(*api.ActionResult) error { return nil },
	}

	// Assert that the GetWorkerID method returns the expected ID
	assert.Equal(t, workerID, workerCtx.GetWorkerID())
}

func TestDefaultWorkerContext_SendStatus_NonFinalState(t *testing.T) {
	// Set up a mock send function to capture the result
	var capturedResult *api.ActionResult
	mockSendFunc := func(result *api.ActionResult) error {
		capturedResult = result
		return nil
	}

	// Create a worker context with the mock send function
	workerCtx := &defaultBridgeWorkerContext{
		ctx:        context.Background(),
		serverURL:  "https://example.com",
		workerID:   "worker-123",
		sendResult: mockSendFunc,
	}

	// Create an action result in a non-final state (None)
	result := &api.ActionResult{
		UnitID:            uuid.MustParse("5837950a-619e-44da-9b75-f957c2aee14c"),
		SpaceID:           uuid.MustParse("c73bbc39-7ad1-4f32-aba0-1ef0789c9571"),
		QueuedOperationID: uuid.MustParse("4f7003ac-bd9f-4ff3-9edc-0d260fcb1266"),
		ActionResultBaseMeta: api.ActionResultBaseMeta{
			RevisionNum: 1,
			Action:      "Apply",
			Result:      api.ActionResultNone,
			Status:      "Running",
			Message:     "In progress",
			StartedAt:   time.Now(),
		},
	}

	// Send the result
	err := workerCtx.SendStatus(result)

	// Assert that no error occurred
	assert.NoError(t, err)
	// Assert that the TerminatedAt field was not set (since Result is None)
	assert.Nil(t, capturedResult.TerminatedAt)
}

func TestDefaultWorkerContext_SendStatus_FinalState(t *testing.T) {
	// Set up a mock send function to capture the result
	var capturedResult *api.ActionResult
	mockSendFunc := func(result *api.ActionResult) error {
		capturedResult = result
		return nil
	}

	// Create a worker context with the mock send function
	workerCtx := &defaultBridgeWorkerContext{
		ctx:        context.Background(),
		serverURL:  "https://example.com",
		workerID:   "worker-123",
		sendResult: mockSendFunc,
	}

	// Create an action result in a final state (not None)
	result := &api.ActionResult{
		UnitID:            uuid.MustParse("5837950a-619e-44da-9b75-f957c2aee14c"),
		SpaceID:           uuid.MustParse("c73bbc39-7ad1-4f32-aba0-1ef0789c9571"),
		QueuedOperationID: uuid.MustParse("4f7003ac-bd9f-4ff3-9edc-0d260fcb1266"),
		ActionResultBaseMeta: api.ActionResultBaseMeta{
			RevisionNum: 1,
			Action:      "Apply",
			Result:      api.ActionResultApplyCompleted,
			Status:      "Completed",
			Message:     "Successfully applied",
			StartedAt:   time.Now(),
		},
	}

	// Get the time before sending the result
	beforeSend := time.Now()

	// Send the result
	err := workerCtx.SendStatus(result)

	// Get the time after sending the result
	afterSend := time.Now()

	// Assert that no error occurred
	assert.NoError(t, err)
	// Assert that the TerminatedAt field was set
	assert.NotNil(t, capturedResult.TerminatedAt)
	// Assert that the TerminatedAt field is between beforeSend and afterSend
	assert.True(t, !capturedResult.TerminatedAt.Before(beforeSend) && !capturedResult.TerminatedAt.After(afterSend))
}

func TestDefaultWorkerContext_SendStatus_Error(t *testing.T) {
	// Create an error to be returned by the mock send function
	expectedErr := errors.New("send error")
	mockSendFunc := func(result *api.ActionResult) error {
		return expectedErr
	}

	// Create a worker context with the mock send function
	workerCtx := &defaultBridgeWorkerContext{
		ctx:        context.Background(),
		serverURL:  "https://example.com",
		workerID:   "worker-123",
		sendResult: mockSendFunc,
	}

	// Create an action result
	result := &api.ActionResult{
		UnitID:            uuid.MustParse("5837950a-619e-44da-9b75-f957c2aee14c"),
		SpaceID:           uuid.MustParse("c73bbc39-7ad1-4f32-aba0-1ef0789c9571"),
		QueuedOperationID: uuid.MustParse("4f7003ac-bd9f-4ff3-9edc-0d260fcb1266"),
		ActionResultBaseMeta: api.ActionResultBaseMeta{
			RevisionNum: 1,
			Action:      "Apply",
			Result:      api.ActionResultNone,
			Status:      "Running",
			Message:     "In progress",
			StartedAt:   time.Now(),
		},
	}

	// Send the result and capture the error
	err := workerCtx.SendStatus(result)

	// Assert that the expected error was returned
	assert.Equal(t, expectedErr, err)
}

// Test that the defaultBridgeWorkerContext properly implements the BridgeWorkerContext interface
func TestDefaultWorkerContext_ImplementsWorkerContext(t *testing.T) {
	var _ api.BridgeWorkerContext = (*defaultBridgeWorkerContext)(nil)
}
