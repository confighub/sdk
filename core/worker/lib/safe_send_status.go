// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package lib

import (
	"fmt"
	"log/slog"

	"github.com/confighub/sdk/core/worker/api"
)

// SafeSendStatus wraps the SendStatus call, logs errors, and returns a wrapped error if applicable.
func SafeSendStatus(wctx api.BridgeWorkerContext, status *api.ActionResult, originalErr error) error {
	err := wctx.SendStatus(status)
	if err != nil {
		slog.Error("Failed to send status", "error", err, "status", status)

		// Wrap the error with the original error if it exists
		if originalErr != nil {
			return fmt.Errorf("original error: %v, send status error: %w", originalErr, err)
		}
		return err
	}

	// If no error occurred in SendStatus, return the original error (if any)
	return originalErr
}
