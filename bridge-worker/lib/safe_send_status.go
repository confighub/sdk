// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package lib

import (
	"fmt"

	"github.com/confighub/sdk/bridge-worker/api"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// SafeSendStatus wraps the SendStatus call, logs errors, and returns a wrapped error if applicable.
func SafeSendStatus(wctx api.BridgeWorkerContext, status *api.ActionResult, originalErr error) error {
	err := wctx.SendStatus(status)
	if err != nil {
		log.Log.Error(err, "Failed to send status", "status", status)

		// Wrap the error with the original error if it exists
		if originalErr != nil {
			return fmt.Errorf("original error: %v, send status error: %w", originalErr, err)
		}
		return err
	}

	// If no error occurred in SendStatus, return the original error (if any)
	return originalErr
}
