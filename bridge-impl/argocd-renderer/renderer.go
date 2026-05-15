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
	// ArgoCDTrackingIDAnnotation is the annotation ArgoCD adds to track resources.
	ArgoCDTrackingIDAnnotation = "argocd.argoproj.io/tracking-id"
	// ArgoCDRefreshAnnotation triggers ArgoCD to re-fetch sources from git.
	ArgoCDRefreshAnnotation = "argocd.argoproj.io/refresh"
	// ArgoCDHydrateAnnotation triggers ArgoCD to re-hydrate rendered manifests.
	ArgoCDHydrateAnnotation = "argocd.argoproj.io/hydrate"
	// createNamespaceSyncOption is the ArgoCD sync option that asks ArgoCD
	// to create the destination namespace if it does not exist. The
	// renderer detects this on the source Application and emits a
	// Namespace document into the rendered output so the stored
	// configuration is self-contained.
	createNamespaceSyncOption = "CreateNamespace=true"
)

// DestinationSettings captures the destination-related fields the renderer
// bakes into the rendered output so the manifests stored in ConfigHub are
// self-contained: downstream apply paths do not need to re-derive a
// destination namespace at apply time, and the deployment bridge does not
// need a DestinationNamespace option.
type DestinationSettings struct {
	// Namespace is spec.destination.namespace from the source Application.
	// When non-empty, the renderer sets metadata.namespace on every
	// namespaced resource in the rendered output that does not already
	// have one.
	Namespace string

	// CreateNamespace is true when the source Application's syncOptions
	// (either spec.syncPolicy.syncOptions or operation.sync.syncOptions)
	// contains "CreateNamespace=true". When true and Namespace is
	// non-empty, the renderer prepends a Namespace document to the
	// rendered output.
	CreateNamespace bool
}

// ExtractDestinationSettings reads namespace-related settings from an
// ArgoCD Application. The renderer uses these to bake namespace
// information into the rendered output. Returns a zero value when the
// Application has neither a destination namespace nor a CreateNamespace
// sync option (the renderer then leaves the manifests untouched).
func ExtractDestinationSettings(app *unstructured.Unstructured) DestinationSettings {
	ns, _, _ := unstructured.NestedString(app.Object, "spec", "destination", "namespace")

	createNS := false
	for _, path := range [][]string{
		{"spec", "syncPolicy", "syncOptions"},
		{"operation", "sync", "syncOptions"},
	} {
		opts, found, _ := unstructured.NestedStringSlice(app.Object, path...)
		if !found {
			continue
		}
		for _, opt := range opts {
			if opt == createNamespaceSyncOption {
				createNS = true
				break
			}
		}
		if createNS {
			break
		}
	}

	return DestinationSettings{
		Namespace:       ns,
		CreateNamespace: createNS,
	}
}

// NOTE: The results of rendering aren't being deployed as the renderer unit, so UnitSlug and SpaceID
// annotations should not be added by the renderer. They will be added by the deployment bridge.

// RenderArgoCD renders an ArgoCD Application to Kubernetes manifests.
// It creates/updates the Application in the cluster (with auto-sync disabled),
// then calls the ArgoCD API to get the rendered manifests.
//
// The Application's destination settings (spec.destination.namespace and the
// CreateNamespace sync option) are baked into the rendered output so the
// stored configuration is self-contained.
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

	return renderFromApplication(ctx, k8sClient, app, config)
}

// RenderExistingArgoCD renders manifests for an ArgoCD Application that
// already exists in the cluster. Unlike RenderArgoCD, it does not create or
// update the Application. The caller passes the in-cluster Application so
// destination settings can be extracted from its spec.
func RenderExistingArgoCD(ctx context.Context, k8sClient client.Client, app *unstructured.Unstructured, config Config) (*RenderResult, error) {
	return renderFromApplication(ctx, k8sClient, app, config)
}

// renderFromApplication is the shared body of RenderArgoCD and
// RenderExistingArgoCD: fetch manifests from ArgoCD's API, then bake the
// Application's destination namespace and CreateNamespace setting into
// the rendered output.
func renderFromApplication(ctx context.Context, k8sClient client.Client, app *unstructured.Unstructured, config Config) (*RenderResult, error) {
	appName := app.GetName()
	appNamespace := app.GetNamespace()
	if appNamespace == "" {
		appNamespace = "argocd"
	}

	manifestResp, err := getManifestsWithRetry(ctx, config, appName, appNamespace)
	if err != nil {
		return nil, fmt.Errorf("failed to get manifests from ArgoCD: %w", err)
	}

	settings := ExtractDestinationSettings(app)
	yamlBytes, err := convertManifestsToYAML(k8sClient, manifestResp.Manifests, settings)
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

// DisableAutoSync removes spec.syncPolicy.automated to prevent ArgoCD from deploying.
// If spec.syncPolicy is then empty, it is removed as well.
func DisableAutoSync(app *unstructured.Unstructured) {
	unstructured.RemoveNestedField(app.Object, "spec", "syncPolicy", "automated")
	if syncPolicy, found, _ := unstructured.NestedMap(app.Object, "spec", "syncPolicy"); found && len(syncPolicy) == 0 {
		unstructured.RemoveNestedField(app.Object, "spec", "syncPolicy")
	}
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

// convertManifestsToYAML converts JSON manifest strings to a multi-document
// YAML byte slice. For each rendered resource it strips the ArgoCD
// tracking-id annotation, and — when the source Application named a
// destination namespace — sets metadata.namespace on namespaced resources
// that did not already have one. When CreateNamespace=true was set on the
// source Application a Namespace document is prepended to the output so
// the stored configuration carries the namespace creation intent.
func convertManifestsToYAML(k8sClient client.Client, manifests []string, settings DestinationSettings) ([]byte, error) {
	var docs []string

	if settings.CreateNamespace && settings.Namespace != "" {
		nsDoc, err := marshalNamespaceDoc(settings.Namespace)
		if err != nil {
			return nil, err
		}
		docs = append(docs, nsDoc)
	}

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

		// Bake the destination namespace into namespaced resources that
		// don't already have one. ArgoCD itself applies destination at
		// sync time; ConfigHub stores rendered output and needs the
		// manifests to be self-contained.
		if settings.Namespace != "" && obj.GetNamespace() == "" {
			if isNamespaced, err := k8sClient.IsObjectNamespaced(obj); err == nil && isNamespaced {
				obj.SetNamespace(settings.Namespace)
			}
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

// marshalNamespaceDoc renders a minimal Namespace document so the
// resulting bundle can stand on its own when applied via a deployment
// bridge (no need for the bridge to inject CreateNamespace=true sync
// options on its own).
func marshalNamespaceDoc(name string) (string, error) {
	ns := map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "Namespace",
		"metadata":   map[string]interface{}{"name": name},
	}
	yamlBytes, err := sigsyaml.Marshal(ns)
	if err != nil {
		return "", fmt.Errorf("failed to marshal Namespace doc: %w", err)
	}
	return string(yamlBytes), nil
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
