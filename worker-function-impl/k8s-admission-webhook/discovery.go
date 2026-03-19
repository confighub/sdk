// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package admissionwebhook

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/cockroachdb/errors"
)

// K8sDiscoveryClient manages connections to the K8s API for VWC discovery.
type K8sDiscoveryClient struct {
	httpClient *http.Client
	apiHost    string
	token      string
}

// NewK8sDiscoveryClient creates a client using in-cluster config or kubeconfig.
// It first tries in-cluster config (service account token + CA cert), then
// falls back to the default kubeconfig file.
func NewK8sDiscoveryClient() (*K8sDiscoveryClient, error) {
	// Try in-cluster config first.
	tokenBytes, tokenErr := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/token")
	if tokenErr == nil {
		token := strings.TrimSpace(string(tokenBytes))

		caCertBytes, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/ca.crt")
		if err != nil {
			return nil, errors.Wrap(err, "failed to read cluster CA cert")
		}

		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caCertBytes) {
			return nil, errors.New("failed to parse cluster CA cert")
		}

		apiHost := os.Getenv("KUBERNETES_SERVICE_HOST")
		apiPort := os.Getenv("KUBERNETES_SERVICE_PORT")
		if apiHost == "" || apiPort == "" {
			return nil, errors.New("KUBERNETES_SERVICE_HOST/PORT not set (not running in-cluster?)")
		}

		return &K8sDiscoveryClient{
			httpClient: &http.Client{
				Transport: &http.Transport{
					TLSClientConfig: &tls.Config{
						RootCAs:    pool,
						MinVersion: tls.VersionTLS12,
					},
				},
			},
			apiHost: fmt.Sprintf("https://%s:%s", apiHost, apiPort),
			token:   token,
		}, nil
	}

	// Fall back to kubeconfig.
	return newK8sDiscoveryClientFromKubeconfig()
}

// NewK8sDiscoveryClientForTesting creates a discovery client for testing.
func NewK8sDiscoveryClientForTesting(httpClient *http.Client, apiHost, token string) *K8sDiscoveryClient {
	return &K8sDiscoveryClient{
		httpClient: httpClient,
		apiHost:    apiHost,
		token:      token,
	}
}

// DiscoverWebhooks queries the K8s API for VWCs matching the selector.
func (c *K8sDiscoveryClient) DiscoverWebhooks(selector WebhookSelector) ([]WebhookEndpoint, error) {
	url := c.apiHost + "/apis/admissionregistration.k8s.io/v1/validatingwebhookconfigurations"
	if selector.LabelSelector != "" {
		url += "?labelSelector=" + selector.LabelSelector
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create request")
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list ValidatingWebhookConfigurations")
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, errors.Wrap(err, "failed to read Kubernetes API response")
	}

	if resp.StatusCode != http.StatusOK {
		return nil, errors.Newf("Kubernetes API returned HTTP %d: %s", resp.StatusCode, string(body))
	}

	var list VWCList
	if err := json.Unmarshal(body, &list); err != nil {
		return nil, errors.Wrapf(err, "failed to parse ValidatingWebhookConfigurations response")
	}

	return ParseWebhookEndpoints(list, selector), nil
}

// discoverWithResourceVersion queries VWCs and returns both the endpoints and the resourceVersion for watch.
func (c *K8sDiscoveryClient) discoverWithResourceVersion(selector WebhookSelector) ([]WebhookEndpoint, string, error) {
	url := c.apiHost + "/apis/admissionregistration.k8s.io/v1/validatingwebhookconfigurations"
	if selector.LabelSelector != "" {
		url += "?labelSelector=" + selector.LabelSelector
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, "", errors.Wrap(err, "failed to create request")
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, "", errors.Wrap(err, "failed to list ValidatingWebhookConfigurations")
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", errors.Wrap(err, "failed to read Kubernetes API response")
	}

	if resp.StatusCode != http.StatusOK {
		return nil, "", errors.Newf("Kubernetes API returned HTTP %d: %s", resp.StatusCode, string(body))
	}

	var listResp struct {
		Metadata struct {
			ResourceVersion string `json:"resourceVersion"`
		} `json:"metadata"`
		Items []VWCItem `json:"items"`
	}
	if err := json.Unmarshal(body, &listResp); err != nil {
		return nil, "", errors.Wrapf(err, "failed to parse ValidatingWebhookConfigurations response")
	}

	list := VWCList{Items: listResp.Items}
	return ParseWebhookEndpoints(list, selector), listResp.Metadata.ResourceVersion, nil
}

// WebhookWatcher watches for VWC updates via K8s watch API.
type WebhookWatcher struct {
	client    *K8sDiscoveryClient
	selector  WebhookSelector
	endpoints []WebhookEndpoint
	mu        sync.RWMutex
}

// NewWebhookWatcher creates a new watcher that keeps webhook endpoints up to date.
func NewWebhookWatcher(client *K8sDiscoveryClient, selector WebhookSelector) *WebhookWatcher {
	return &WebhookWatcher{
		client:   client,
		selector: selector,
	}
}

// Start performs an initial list and then watches for updates in a background goroutine.
// It blocks until the initial list completes, then returns.
func (w *WebhookWatcher) Start(ctx context.Context) error {
	endpoints, resourceVersion, err := w.client.discoverWithResourceVersion(w.selector)
	if err != nil {
		return errors.Wrap(err, "initial webhook discovery failed")
	}

	w.mu.Lock()
	w.endpoints = endpoints
	w.mu.Unlock()

	slog.Info("webhook watcher started", "endpoints", len(endpoints))

	go w.watchLoop(ctx, resourceVersion)
	return nil
}

// GetEndpoints returns the current set of webhook endpoints (thread-safe).
func (w *WebhookWatcher) GetEndpoints() []WebhookEndpoint {
	w.mu.RLock()
	defer w.mu.RUnlock()
	result := make([]WebhookEndpoint, len(w.endpoints))
	copy(result, w.endpoints)
	return result
}

const relistInterval = 30 * time.Second

func (w *WebhookWatcher) watchLoop(ctx context.Context, resourceVersion string) {
	for {
		if ctx.Err() != nil {
			return
		}

		err := w.watch(ctx, resourceVersion)
		if err != nil {
			slog.Warn("webhook watch error, falling back to re-list", "error", err)
		}

		// Re-list to get fresh data and resourceVersion.
		select {
		case <-ctx.Done():
			return
		case <-time.After(relistInterval):
		}

		endpoints, rv, err := w.client.discoverWithResourceVersion(w.selector)
		if err != nil {
			slog.Warn("webhook re-list failed", "error", err)
			continue
		}

		w.mu.Lock()
		w.endpoints = endpoints
		w.mu.Unlock()
		resourceVersion = rv
		slog.Info("webhook watcher re-listed", "endpoints", len(endpoints))
	}
}

// watchEvent represents a single event from the K8s watch API.
type watchEvent struct {
	Type   string          `json:"type"`
	Object json.RawMessage `json:"object"`
}

func (w *WebhookWatcher) watch(ctx context.Context, resourceVersion string) error {
	url := w.client.apiHost + "/apis/admissionregistration.k8s.io/v1/validatingwebhookconfigurations?watch=true"
	if w.selector.LabelSelector != "" {
		url += "&labelSelector=" + w.selector.LabelSelector
	}
	if resourceVersion != "" {
		url += "&resourceVersion=" + resourceVersion
	}

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return errors.Wrap(err, "failed to create watch request")
	}
	req.Header.Set("Authorization", "Bearer "+w.client.token)
	req.Header.Set("Accept", "application/json")

	resp, err := w.client.httpClient.Do(req)
	if err != nil {
		return errors.Wrap(err, "watch request failed")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return errors.Newf("watch returned HTTP %d: %s", resp.StatusCode, string(body))
	}

	decoder := json.NewDecoder(resp.Body)
	for {
		var event watchEvent
		if err := decoder.Decode(&event); err != nil {
			if ctx.Err() != nil {
				return nil // context cancelled, normal shutdown
			}
			return errors.Wrap(err, "watch stream decode error")
		}

		// On any event, re-list to get consistent state.
		// This is simpler and more robust than trying to incrementally
		// update the endpoint list from individual events.
		slog.Info("webhook watch event received", "type", event.Type)
		endpoints, _, listErr := w.client.discoverWithResourceVersion(w.selector)
		if listErr != nil {
			slog.Warn("re-list after watch event failed", "error", listErr)
			continue
		}
		w.mu.Lock()
		w.endpoints = endpoints
		w.mu.Unlock()
	}
}
