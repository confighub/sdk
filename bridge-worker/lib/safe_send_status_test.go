// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package lib

import (
	"errors"
	"testing"

	"github.com/confighub/sdk/bridge-worker/api"
	"github.com/stretchr/testify/assert"
)

func TestSafeSendStatus_Success(t *testing.T) {
	mockCtx := new(MockWorkerContext)
	status := &api.ActionResult{
		ActionResultBaseMeta: api.ActionResultBaseMeta{
			Status:  api.ActionStatusCompleted,
			Result:  api.ActionResultApplyCompleted,
			Message: "Successfully applied",
		},
	}

	// Mock SendStatus to succeed
	mockCtx.On("SendStatus", status).Return(nil)

	// Call SafeSendStatus
	err := SafeSendStatus(mockCtx, status, nil)

	// Assert that SendStatus was called
	mockCtx.AssertCalled(t, "SendStatus", status)

	// Assert that no error occurred
	assert.NoError(t, err)
}

func TestSafeSendStatus_FailureWithOriginalError(t *testing.T) {
	mockCtx := new(MockWorkerContext)
	status := &api.ActionResult{
		ActionResultBaseMeta: api.ActionResultBaseMeta{
			Status:  api.ActionStatusFailed,
			Result:  api.ActionResultApplyFailed,
			Message: "Failed to apply",
		},
	}

	// Mock SendStatus to fail
	mockCtx.On("SendStatus", status).Return(errors.New("network error"))

	// Call SafeSendStatus with an original error
	originalErr := errors.New("original error")
	err := SafeSendStatus(mockCtx, status, originalErr)

	// Assert that SendStatus was called
	mockCtx.AssertCalled(t, "SendStatus", status)

	// Assert that the error is wrapped correctly
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "original error")
	assert.Contains(t, err.Error(), "network error")
}

func TestSafeSendStatus_FailureWithoutOriginalError(t *testing.T) {
	mockCtx := new(MockWorkerContext)
	status := &api.ActionResult{
		ActionResultBaseMeta: api.ActionResultBaseMeta{
			Status:  api.ActionStatusFailed,
			Result:  api.ActionResultApplyFailed,
			Message: "Failed to apply",
		},
	}

	// Mock SendStatus to fail
	mockCtx.On("SendStatus", status).Return(errors.New("network error"))

	// Call SafeSendStatus without an original error
	err := SafeSendStatus(mockCtx, status, nil)

	// Assert that SendStatus was called
	mockCtx.AssertCalled(t, "SendStatus", status)

	// Assert that the error is returned directly
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "network error")
}
