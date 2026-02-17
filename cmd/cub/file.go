// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/cockroachdb/errors"
)

func readFile(fileName string) []byte {
	data, err := os.ReadFile(fileName)
	failOnError(err)
	return data
}

func readStdin() ([]byte, error) {
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func fetchContent(source string) ([]byte, error) {
	if source == "" {
		return nil, errors.New("source cannot be empty")
	}

	// Handle stdin
	if source == "-" {
		return readStdin()
	}

	// Handle file:// URLs
	if filePath, found := strings.CutPrefix(source, "file://"); found {
		data, err := os.ReadFile(filePath)
		if err != nil {
			return nil, fmt.Errorf("failed to read file %s: %w", filePath, err)
		}
		return data, nil
	}

	// Handle HTTPS URLs only
	if strings.HasPrefix(source, "https://") {
		return fetchWithHTTP(source)
	}

	// Handle local files (backward compatibility - no prefix)
	if !strings.Contains(source, "://") {
		data, err := os.ReadFile(source)
		if err != nil {
			return nil, err
		}
		return data, nil
	}

	return nil, fmt.Errorf("unsupported URL scheme: %s (only file:// and https:// are supported)", source)
}

func fetchWithHTTP(source string) ([]byte, error) {
	// Only allow HTTPS URLs for security
	if !strings.HasPrefix(source, "https://") {
		return nil, fmt.Errorf("only HTTPS URLs are supported, got: %s", source)
	}

	// Create HTTP client with timeout
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	// Make HTTP request
	resp, err := client.Get(source)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch %s: %w", source, err)
	}
	defer resp.Body.Close()

	// Check HTTP status
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch %s, Code: %d, Status: %s", source, resp.StatusCode, resp.Status)
	}

	// Read response body
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response from %s: %w", source, err)
	}

	return data, nil
}
