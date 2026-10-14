// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package cubapi

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

// An operation that reports failure in its own response type is read through its Error, and a
// response that has only a Message, as one written before Error, through that.
func TestErrorResponseMessage(t *testing.T) {
	message, ok := errorResponseMessage(&goclientnew.StandardErrorResponse{Message: "standard"})
	assert.True(t, ok)
	assert.Equal(t, "standard", message)

	message, ok = errorResponseMessage(&goclientnew.ReleasePublishResponse{
		Message: "repeated", Error: &goclientnew.ResponseError{Message: "refused"},
	})
	assert.True(t, ok)
	assert.Equal(t, "refused", message)

	message, ok = errorResponseMessage(&goclientnew.ReleasePublishResponse{Message: "only a message"})
	assert.True(t, ok)
	assert.Equal(t, "only a message", message)

	_, ok = errorResponseMessage(&goclientnew.Release{})
	assert.False(t, ok, "a type with neither Error nor Message")
	_, ok = errorResponseMessage("not a struct")
	assert.False(t, ok)
}

// A response's error type tells apart errors that share a status, such as a version another
// write moved past and a name another entity has, which are both 409s.
func TestErrorType(t *testing.T) {
	conflict := func(errorType string) *goclientnew.UpdateTriggerResponse {
		return &goclientnew.UpdateTriggerResponse{
			HTTPResponse: &http.Response{StatusCode: http.StatusConflict},
			JSON409:      &goclientnew.StandardErrorResponse{Error: &goclientnew.ResponseError{Type: errorType}},
		}
	}
	assert.Equal(t, "Conflict", ErrorType(conflict("Conflict")))
	assert.Equal(t, "AlreadyExists", ErrorType(conflict("AlreadyExists")))

	undeclared := &goclientnew.UpdateTriggerResponse{
		HTTPResponse: &http.Response{StatusCode: http.StatusLocked},
		JSONDefault:  &goclientnew.StandardErrorResponse{Error: &goclientnew.ResponseError{Type: "Locked"}},
	}
	assert.Equal(t, "Locked", ErrorType(undeclared), "a status the operation does not declare")

	older := &goclientnew.UpdateTriggerResponse{
		HTTPResponse: &http.Response{StatusCode: http.StatusConflict},
		JSON409:      &goclientnew.StandardErrorResponse{Message: "conflict"},
	}
	assert.Empty(t, ErrorType(older), "a server that sends only a message")
	assert.Empty(t, ErrorType(&goclientnew.UpdateTriggerResponse{HTTPResponse: &http.Response{StatusCode: http.StatusOK}}))
	assert.Empty(t, ErrorType((*goclientnew.UpdateTriggerResponse)(nil)))
}
