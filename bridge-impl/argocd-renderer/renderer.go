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

	"github.com/confighub/sdk/core/configkit/yamlkit"
)

const (
	argoCDAPIVersion = "argoproj.io/v1alpha1"
	argoCDKind       = "Application"
	// ArgoCDTrackingIDAnnotation is the annotation ArgoCD adds to track resources.
	ArgoCDTrackingIDAnnotation = "argocd.argoproj.io/tracking-id"
	// ArgoCDRefreshAnnotation triggers ArgoCD to re-fetch sources from git.
	ArgoCDRefreshAnnotation = "argocd.argoproj.io/refresh"
	// ArgoCDHydrateAnnotation triggers ArgoCD to re-hydrate rendered manifests.
	ArgoCDHydrateAnnotation = "argocd.argoproj.io/hydrate"
)

// NOTE: The results of rendering aren't being deployed as the renderer unit, so UnitSlug and SpaceID
// annotations should not be added by the renderer. They will be added by the deployment bridge.

// RenderArgoCD renders an ArgoCD Application to Kubernetes manifests.
// It creates/updates the Application in the cluster (with auto-sync disabled),
// then calls the ArgoCD API to get the rendered manifests.
func RenderArgoCD(ctx context.Context, appYAML []byte, k8sClient client.Client, config Config) (*RenderResult, error) {
	app, err := ParseApplication(appYAML)
	if err != nil {
		return nil, fmt.Errorf("failed to parse Application: %w", err)
	}

	DisableAutoSync(app)
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

	yamlBytes, err := convertManifestsToYAML(k8sClient, manifestResp.Manifests)
	if err != nil {
		return nil, fmt.Errorf("failed to convert manifests to YAML: %w", err)
	}

	return &RenderResult{
		Manifests:  yamlBytes,
		Revision:   manifestResp.Revision,
		SourceType: manifestResp.SourceType,
	}, nil
}

// RenderExistingArgoCD renders manifests for an ArgoCD Application that already exists in the cluster.
// Unlike RenderArgoCD, it does not create or update the Application.
func RenderExistingArgoCD(ctx context.Context, k8sClient client.Client, appName, appNamespace string, config Config) (*RenderResult, error) {
	manifestResp, err := getManifestsWithRetry(ctx, config, appName, appNamespace)
	if err != nil {
		return nil, fmt.Errorf("failed to get manifests from ArgoCD: %w", err)
	}

	yamlBytes, err := convertManifestsToYAML(k8sClient, manifestResp.Manifests)
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

// DisableAutoSync sets spec.syncPolicy.automated.enabled to false to prevent ArgoCD
// from deploying. This preserves the rest of the automated policy (prune, selfHeal, etc.)
// while ensuring the controller skips automated sync.
func DisableAutoSync(app *unstructured.Unstructured) {
	// Ensure the automated map exists before setting enabled
	automated, found, _ := unstructured.NestedMap(app.Object, "spec", "syncPolicy", "automated")
	if !found || automated == nil {
		// No automated policy — nothing to disable
		return
	}
	unstructured.SetNestedField(app.Object, false, "spec", "syncPolicy", "automated", "enabled")
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
// It strips the ArgoCD tracking-id annotation from each resource and adds a placeholder
// namespace to namespaced resources.
func convertManifestsToYAML(k8sClient client.Client, manifests []string) ([]byte, error) {
	var docs []string

	for i, manifest := range manifests {
		// Parse JSON into unstructured to remove tracking annotation
		obj := &unstructured.Unstructured{}
		if err := obj.UnmarshalJSON([]byte(manifest)); err != nil {
			return nil, fmt.Errorf("failed to parse manifest %d: %w", i, err)
		}

		// Remove the ArgoCD tracking-id annotation
		annotations := obj.GetAnnotations()
		if _, ok := annotations[ArgoCDTrackingIDAnnotation]; ok {
			delete(annotations, ArgoCDTrackingIDAnnotation)
			if len(annotations) == 0 {
				annotations = nil
			}
			obj.SetAnnotations(annotations)
		}

		// Set placeholder namespace on namespaced resources
		if isNamespaced, err := k8sClient.IsObjectNamespaced(obj); err == nil && isNamespaced {
			obj.SetNamespace(yamlkit.PlaceHolderBlockApplyString)
		}

		// Marshal back to JSON, then convert to YAML
		jsonBytes, err := obj.MarshalJSON()
		if err != nil {
			return nil, fmt.Errorf("failed to marshal manifest %d to JSON: %w", i, err)
		}
		yamlBytes, err := sigsyaml.JSONToYAML(jsonBytes)
		if err != nil {
			return nil, fmt.Errorf("failed to convert manifest %d to YAML: %w", i, err)
		}
		docs = append(docs, string(yamlBytes))
	}

	return []byte(strings.Join(docs, "---\n")), nil
}

// SetRefreshAnnotations adds refresh and hydrate annotations to an Application
// to trigger ArgoCD to re-fetch sources from git and re-render manifests.
func SetRefreshAnnotations(app *unstructured.Unstructured) {
	annotations := app.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	}
	annotations[ArgoCDRefreshAnnotation] = "normal"
	annotations[ArgoCDHydrateAnnotation] = "normal"
	app.SetAnnotations(annotations)
}

// PatchRefreshAnnotations patches the refresh and hydrate annotations on an existing
// Application resource in the cluster to trigger ArgoCD to re-fetch and re-render.
func PatchRefreshAnnotations(ctx context.Context, k8sClient client.Client, appName, appNamespace string) error {
	app := &unstructured.Unstructured{}
	app.SetAPIVersion(argoCDAPIVersion)
	app.SetKind(argoCDKind)
	app.SetName(appName)
	app.SetNamespace(appNamespace)

	patch := []byte(fmt.Sprintf(`{"metadata":{"annotations":{%q:"normal",%q:"normal"}}}`,
		ArgoCDRefreshAnnotation, ArgoCDHydrateAnnotation))

	return k8sClient.Patch(ctx, app, client.RawPatch(types.MergePatchType, patch))
}

// HasAutoSync checks whether an ArgoCD Application has autosync enabled.
//
// Per ArgoCD docs, autosync is enabled when:
//   - spec.syncPolicy.automated is present (even if empty: `automated: {}`)
//   - AND spec.syncPolicy.automated.enabled is NOT explicitly set to false
//
// Setting enabled to null or omitting it is treated as enabled.
// Setting enabled to false disables autosync even if prune/selfHeal/allowEmpty are set.
func HasAutoSync(app *unstructured.Unstructured) bool {
	automated, found, _ := unstructured.NestedMap(app.Object, "spec", "syncPolicy", "automated")
	if !found || automated == nil {
		return false
	}

	// Check if enabled is explicitly set to false
	enabled, found, _ := unstructured.NestedBool(app.Object, "spec", "syncPolicy", "automated", "enabled")
	if found && !enabled {
		return false
	}

	return true
}
