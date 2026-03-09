// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package cubapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

func PerformWorkerAuth(serverURL, workerID, workerSecret string) (*AuthSession, error) {
	// Create request body
	requestBody := map[string]string{
		"worker_id":     workerID,
		"worker_secret": workerSecret,
	}

	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request body: %w", err)
	}

	// Make authentication request
	endpoint := fmt.Sprintf("%s/auth/worker", serverURL)
	req, err := http.NewRequest("POST", endpoint, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make authentication request: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(res.Body)
		return nil, fmt.Errorf("authentication failed: %s", string(body))
	}

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	session := AuthSession{}
	session.AuthType = "Bearer"

	if err := json.Unmarshal(body, &session); err != nil {
		return nil, fmt.Errorf("failed to parse authentication response: %w", err)
	}
	return &session, nil
}
