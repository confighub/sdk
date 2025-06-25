// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package client

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/cockroachdb/errors"

	"github.com/confighub/sdk/function/api"
	"github.com/confighub/sdk/workerapi"
)


func InvokeFunctions(
	transportConfig *TransportConfig,
	toolchain workerapi.ToolchainType,
	reqMsg api.FunctionInvocationRequest,
) (*api.FunctionInvocationResponse, error) {
	// Create the request
	var err error
	if reqMsg.FunctionContext.PreviousContentHash == api.RevisionHash(0) {
		reqMsg.PreviousContentHash = api.HashConfigData(reqMsg.ConfigData)
	}
	reqMsg.ToolchainType = toolchain
	marshaled, err := json.Marshal(reqMsg)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	// Send the request
	url := transportConfig.GetToolchainURL(toolchain)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, url, bytes.NewReader(marshaled)) //nolint:G107 // dynamic URL for testing
	if err != nil {
		return nil, errors.WithStack(err)
	}
	req.Header.Set("Content-Type", transportConfig.GetContentType())
	req.Header.Set("User-Agent", transportConfig.GetUserAgent())
	client := http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, errors.WithStack(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, errors.WithStack(errors.New(http.StatusText(resp.StatusCode)))
	}

	// Process the response
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, errors.WithStack(err)
	}
	var respMsg api.FunctionInvocationResponse
	err = json.Unmarshal(respBody, &respMsg)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	return &respMsg, nil
}
