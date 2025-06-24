// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package client

import (
	"bytes"
	"context"
	"net/http"
	"time"

	"github.com/cockroachdb/errors"
)

func Ok(transportConfig *TransportConfig) error {
	// Send the request
	url := transportConfig.GetBaseURL() + "/ok"
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, bytes.NewReader([]byte{})) //nolint:G107 // dynamic URL for testing
	if err != nil {
		return errors.WithStack(err)
	}
	req.Header.Set("Content-Type", transportConfig.GetContentType())
	req.Header.Set("User-Agent", transportConfig.GetUserAgent())
	client := http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return errors.WithStack(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return errors.WithStack(errors.New(http.StatusText(resp.StatusCode)))
	}

	return nil
}
