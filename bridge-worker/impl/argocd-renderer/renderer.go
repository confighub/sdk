// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package argocdrenderer

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	sigsyaml "sigs.k8s.io/yaml"
)

const (
	argoCDAPIVersion = "argoproj.io/v1alpha1"
	argoCDKind       = "Application"
)

// RenderArgoCD renders an ArgoCD Application to Kubernetes manifests.
// It creates/updates the Application in the cluster (with auto-sync disabled),
// then calls the ArgoCD API to get the rendered manifests.
func RenderArgoCD(ctx context.Context, appYAML []byte, k8sClient client.Client, config Config) (*RenderResult, error) {
	app, err := ParseApplication(appYAML)
	if err != nil {
		return nil, fmt.Errorf("failed to parse Application: %w", err)
	}

	disableAutoSync(app)
	// TODO: ensureConfigHubContext

	if err := createOrUpdateApplication(ctx, k8sClient, app); err != nil {
		return nil, fmt.Errorf("failed to create/update Application: %w", err)
	}

	appName := app.GetName()
	appNamespace := app.GetNamespace()
	if appNamespace == "" {
		appNamespace = "argocd"
	}

	manifestResp, err := getManifestsWithRetry(ctx, config, appName, appNamespace)
	if err != nil {
		return nil, fmt.Errorf("failed to get manifests from ArgoCD: %w", err)
	}

	yamlBytes, err := convertManifestsToYAML(manifestResp.Manifests)
	if err != nil {
		return nil, fmt.Errorf("failed to convert manifests to YAML: %w", err)
	}

	return &RenderResult{
		Manifests:  yamlBytes,
		Revision:   manifestResp.Revision,
		SourceType: manifestResp.SourceType,
	}, nil
}

// ParseApplication parses YAML bytes into an unstructured Application and validates it.
func ParseApplication(data []byte) (*unstructured.Unstructured, error) {
	jsonData, err := sigsyaml.YAMLToJSON(data)
	if err != nil {
		return nil, fmt.Errorf("failed to convert YAML to JSON: %w", err)
	}

	obj := &unstructured.Unstructured{}
	if err := obj.UnmarshalJSON(jsonData); err != nil {
		return nil, fmt.Errorf("failed to unmarshal JSON: %w", err)
	}

	if obj.GetAPIVersion() != argoCDAPIVersion {
		return nil, fmt.Errorf("expected apiVersion %s, got %s", argoCDAPIVersion, obj.GetAPIVersion())
	}
	if obj.GetKind() != argoCDKind {
		return nil, fmt.Errorf("expected kind %s, got %s", argoCDKind, obj.GetKind())
	}

	return obj, nil
}

// disableAutoSync removes spec.syncPolicy.automated to prevent ArgoCD from deploying.
func disableAutoSync(app *unstructured.Unstructured) {
	unstructured.RemoveNestedField(app.Object, "spec", "syncPolicy", "automated")
}

// createOrUpdateApplication creates the Application in the cluster, or updates it if it exists.
func createOrUpdateApplication(ctx context.Context, k8sClient client.Client, app *unstructured.Unstructured) error {
	logger := log.FromContext(ctx)
	logger.Info("Creating/updating ArgoCD Application", "name", app.GetName(), "namespace", app.GetNamespace())

	err := k8sClient.Create(ctx, app)
	if err == nil {
		return nil
	}

	if apierrors.IsAlreadyExists(err) {
		logger.Info("Application already exists, updating", "name", app.GetName())

		existing := &unstructured.Unstructured{}
		existing.SetAPIVersion(app.GetAPIVersion())
		existing.SetKind(app.GetKind())
		key := types.NamespacedName{
			Namespace: app.GetNamespace(),
			Name:      app.GetName(),
		}
		if err := k8sClient.Get(ctx, key, existing); err != nil {
			return fmt.Errorf("failed to get existing Application: %w", err)
		}

		app.SetResourceVersion(existing.GetResourceVersion())
		return k8sClient.Update(ctx, app)
	}

	return err
}

// getManifestsWithRetry calls the ArgoCD API with retries to handle informer sync lag.
func getManifestsWithRetry(ctx context.Context, config Config, appName, appNamespace string) (*ManifestResponse, error) {
	var resp *ManifestResponse

	err := wait.PollUntilContextTimeout(ctx, 3*time.Second, 2*time.Minute, true, func(ctx context.Context) (bool, error) {
		var err error
		resp, err = getManifests(ctx, config, appName, appNamespace)
		if err != nil {
			log.FromContext(ctx).Info("Retrying manifest fetch", "error", err.Error())
			return false, nil
		}
		return true, nil
	})

	if err != nil {
		return nil, fmt.Errorf("timed out waiting for manifests: %w", err)
	}

	return resp, nil
}

// getManifests calls the ArgoCD REST API to get rendered manifests for an Application.
func getManifests(ctx context.Context, config Config, appName, appNamespace string) (*ManifestResponse, error) {
	scheme := "https"
	if config.Insecure {
		scheme = "http"
	}

	url := fmt.Sprintf("%s://%s/api/v1/applications/%s/manifests?appNamespace=%s", scheme, config.ServerAddress, appName, appNamespace)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+config.AuthToken)

	httpClient := &http.Client{
		Timeout: 60 * time.Second,
	}
	if config.Insecure {
		httpClient.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // User-configured insecure mode
		}
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ArgoCD API returned status %d: %s", resp.StatusCode, string(body))
	}

	var manifestResp ManifestResponse
	if err := json.Unmarshal(body, &manifestResp); err != nil {
		return nil, fmt.Errorf("failed to parse manifest response: %w", err)
	}

	return &manifestResp, nil
}

// convertManifestsToYAML converts JSON manifest strings to a multi-document YAML byte slice.
func convertManifestsToYAML(manifests []string) ([]byte, error) {
	var docs []string

	for i, manifest := range manifests {
		yamlBytes, err := sigsyaml.JSONToYAML([]byte(manifest))
		if err != nil {
			return nil, fmt.Errorf("failed to convert manifest %d to YAML: %w", i, err)
		}
		docs = append(docs, string(yamlBytes))
	}

	return []byte(strings.Join(docs, "---\n")), nil
}
