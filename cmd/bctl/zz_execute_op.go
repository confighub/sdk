// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/confighub/sdk/bridge-worker/api"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

const queuedOperationRoute = "%s/api/bridgeworker/%s/queued-operation"

func executeOperation(operationName string, _ *cobra.Command, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("filename argument required")
	}
	filename := args[0]

	var content []byte
	var err error
	if filename == "-" {
		content, err = io.ReadAll(os.Stdin)
	} else {
		content, err = os.ReadFile(filename)
	}
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	operation := api.BridgeWorkerEventRequest{
		Action: api.ActionType(operationName),
		Payload: api.BridgeWorkerPayload{
			UnitID: uuid.MustParse("5837950a-619e-44da-9b75-f957c2aee14c"), // TODO: Make configurable
			Data:   content,
		},
	}

	url := fmt.Sprintf(queuedOperationRoute,
		rootArgs.configHubURL,
		rootArgs.workerID)

	jsonData, err := json.Marshal(operation)
	if err != nil {
		return fmt.Errorf("failed to marshal operation: %w", err)
	}

	tr := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true,
		},
		ForceAttemptHTTP2: true,
	}
	client := &http.Client{Transport: tr}

	req, err := http.NewRequest(http.MethodPost, url, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", rootArgs.workerSecret))

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("request failed with status %d: %s", resp.StatusCode, body)
	}

	return nil
}
