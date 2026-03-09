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

func GetRegisteredPaths(transportConfig *TransportConfig, toolchain workerapi.ToolchainType) (api.AttributeNameToResourceTypeToPathToVisitorInfoType, error) {
	// Send the request
	url := transportConfig.GetToolchainURL(toolchain) + "/paths"
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, bytes.NewReader([]byte{})) //nolint:G107 // dynamic URL for testing
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
	var respMsg api.AttributeNameToResourceTypeToPathToVisitorInfoType
	err = json.Unmarshal(respBody, &respMsg)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	return respMsg, nil
}
