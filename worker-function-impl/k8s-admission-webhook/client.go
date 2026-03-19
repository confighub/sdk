// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package admissionwebhook

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/cockroachdb/errors"
)

// WebhookClientConfig contains the configuration for creating a WebhookClient.
type WebhookClientConfig struct {
	BaseURL        string
	CACertPath     string
	SkipTLSVerify  bool
}

// WebhookClient sends AdmissionReview requests to a webhook endpoint.
type WebhookClient struct {
	httpClient *http.Client
	baseURL    string
}

// NewWebhookClient creates a new WebhookClient from environment variables.
func NewWebhookClient(urlEnv, caCertPathEnv, skipTLSVerifyEnv string) (*WebhookClient, error) {
	baseURL := os.Getenv(urlEnv)
	if baseURL == "" {
		return nil, fmt.Errorf("%s environment variable is required", urlEnv)
	}
	baseURL = strings.TrimRight(baseURL, "/")

	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}

	if os.Getenv(skipTLSVerifyEnv) == "true" {
		tlsConfig.InsecureSkipVerify = true //nolint:gosec // user-controlled for dev/testing
	}

	if caPath := os.Getenv(caCertPathEnv); caPath != "" {
		caCert, err := os.ReadFile(caPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read CA cert from %s: %w", caPath, err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caCert) {
			return nil, fmt.Errorf("failed to parse CA cert from %s", caPath)
		}
		tlsConfig.RootCAs = pool
	}

	return &WebhookClient{
		httpClient: &http.Client{
			Transport: &http.Transport{TLSClientConfig: tlsConfig},
		},
		baseURL: baseURL,
	}, nil
}

// NewWebhookClientForTesting creates a WebhookClient with a custom HTTP client
// and base URL, suitable for tests using httptest.Server.
func NewWebhookClientForTesting(httpClient *http.Client, baseURL string) *WebhookClient {
	return &WebhookClient{
		httpClient: httpClient,
		baseURL:    baseURL,
	}
}

// CallWebhook sends an AdmissionReview to a specific webhook path and returns the response.
func (c *WebhookClient) CallWebhook(path string, reviewJSON []byte) (*AdmissionResponse, error) {
	resp, err := c.httpClient.Post(c.baseURL+path, "application/json", bytes.NewReader(reviewJSON))
	if err != nil {
		return nil, errors.Wrap(err, "failed to send request to webhook server")
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, errors.Wrap(err, "failed to read webhook response")
	}

	if resp.StatusCode != http.StatusOK {
		return nil, errors.Newf("webhook returned HTTP %d: %s", resp.StatusCode, string(body))
	}

	var reviewResp AdmissionReview
	if err := json.Unmarshal(body, &reviewResp); err != nil {
		return nil, errors.Wrapf(err, "failed to parse webhook response: %s", string(body))
	}

	if reviewResp.Response == nil {
		return nil, errors.Newf("webhook returned empty response")
	}

	return reviewResp.Response, nil
}
