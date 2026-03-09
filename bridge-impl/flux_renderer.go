// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package impl

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/confighub/sdk/bridge-worker/api"
	fluxrenderer "github.com/confighub/sdk/bridge-impl/flux-renderer"
	funcapi "github.com/confighub/sdk/function/api"
	"github.com/confighub/sdk/workerapi"
)

// FluxRendererWorker renders Flux HelmRelease and Kustomization resources
// to Kubernetes manifests without applying them to a cluster.
// It looks up artifact URLs and digests from Flux source controller resources.
//
// When IsAuthoritative=true, Apply sets spec.suspend=true on the Flux resource
// (if it exists) using the Kubernetes client directly, then renders manifests.
// Destroy deletes the resource via the Kubernetes client. Refresh delegates to
// KubernetesBridgeWorker.
//
// NOTE: We do NOT use KubernetesBridgeWorker.Apply because its SSA applier
// waits for resources to become ready. The kustomize-controller skips suspended
// Kustomization resources entirely (never updates status.observedGeneration),
// which causes the applier to hang indefinitely.
//
// When IsAuthoritative=false (default), the Flux resource must already exist in the cluster.
// The bridge only reads rendered manifests without modifying the resource.
type FluxRendererWorker struct {
	KubernetesBridgeWorker KubernetesBridgeWorker
}

var _ api.BridgeWorker = (*FluxRendererWorker)(nil)
var _ api.WatchableWorker = (*FluxRendererWorker)(nil)

func (w *FluxRendererWorker) ID() api.BridgeWorkerID {
	return api.BridgeWorkerID{
		ProviderType:   api.ProviderFluxRenderer,
		ToolchainTypes: []workerapi.ToolchainType{workerapi.ToolchainKubernetesYAML},
	}
}

func (w *FluxRendererWorker) Info(opts api.InfoOptions) api.BridgeWorkerInfo {
	info := w.KubernetesBridgeWorker.InfoForToolchainAndProvider(opts, workerapi.ToolchainKubernetesYAML, api.ProviderFluxRenderer)
	for i := range info.SupportedConfigTypes {
		info.SupportedConfigTypes[i].Options = append(info.SupportedConfigTypes[i].Options, api.BridgeOption{
			Name:        "IsAuthoritative",
			Description: "When true, the bridge creates/updates/deletes the Flux resource and sets spec.suspend to true. When false (default), the resource must already exist and the bridge only reads rendered manifests from it.",
			Required:    false,
			DataType:    funcapi.DataTypeBool,
		})
	}
	return info
}

// isSuspended checks if a Flux resource has spec.suspend set to true.
func isSuspended(obj *unstructured.Unstructured) bool {
	suspended, found, err := unstructured.NestedBool(obj.Object, "spec", "suspend")
	if err != nil || !found {
		return false
	}
	return suspended
}

// findFluxResource extracts the Flux resource (HelmRelease or Kustomization) kind, name,
// namespace, and API version from the payload data.
func findFluxResource(data []byte) (kind, apiVersion, name, namespace string, err error) {
	parsed, err := fluxrenderer.ParseDocumentsWithResources(data)
	if err != nil {
		return "", "", "", "", fmt.Errorf("failed to parse documents: %w", err)
	}

	helmReleases := fluxrenderer.FindAllDocumentsByKindAndAPIVersion(parsed.Documents, fluxrenderer.HelmReleaseKind, fluxrenderer.HelmReleaseAPIVersionPrefix)
	if len(helmReleases) > 0 {
		hr := helmReleases[0]
		return hr.GetKind(), hr.GetAPIVersion(), hr.GetName(), hr.GetNamespace(), nil
	}

	kustomizations := fluxrenderer.FindAllDocumentsByKindAndAPIVersion(parsed.Documents, fluxrenderer.KustomizationKind, fluxrenderer.KustomizationAPIVersionPrefix)
	if len(kustomizations) > 0 {
		ks := kustomizations[0]
		return ks.GetKind(), ks.GetAPIVersion(), ks.GetName(), ks.GetNamespace(), nil
	}

	return "", "", "", "", fmt.Errorf("no HelmRelease or Kustomization found in payload")
}

func (w *FluxRendererWorker) Apply(wctx api.BridgeWorkerContext, payload api.BridgeWorkerPayload) error {
	if !isAuthoritative(payload) {
		return w.applyNonAuthoritative(wctx, payload)
	}

	return w.applyAuthoritative(wctx, payload)
}

// applyAuthoritative ensures spec.suspend=true on the Flux resource (if it exists)
// using the Kubernetes client directly, then renders manifests.
func (w *FluxRendererWorker) applyAuthoritative(wctx api.BridgeWorkerContext, payload api.BridgeWorkerPayload) error {
	var params KubernetesWorkerParams
	if len(payload.TargetParams) > 0 {
		if err := json.Unmarshal(payload.TargetParams, &params); err != nil {
			wctx.SendStatus(newActionResult(
				api.ActionStatusFailed,
				api.ActionResultApplyFailed,
				fmt.Sprintf("failed to parse target parameters: %v", err),
			))
			return fmt.Errorf("failed to parse target parameters: %w", err)
		}
	}

	k8sClient, err := createFluxRendererK8sClient(params.KubeContext)
	if err != nil {
		wctx.SendStatus(newActionResult(
			api.ActionStatusFailed,
			api.ActionResultApplyFailed,
			fmt.Sprintf("failed to create Kubernetes client: %v", err),
		))
		return fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	// Find the Flux resource from payload
	kind, apiVersion, name, namespace, err := findFluxResource(payload.Data)
	if err != nil {
		wctx.SendStatus(newActionResult(
			api.ActionStatusFailed,
			api.ActionResultApplyFailed,
			fmt.Sprintf("failed to find Flux resource: %v", err),
		))
		return fmt.Errorf("failed to find Flux resource: %w", err)
	}

	// If the resource exists in the cluster, ensure spec.suspend is true
	existing := &unstructured.Unstructured{}
	existing.SetAPIVersion(apiVersion)
	existing.SetKind(kind)
	key := types.NamespacedName{Namespace: namespace, Name: name}
	if err := k8sClient.Get(wctx.Context(), key, existing); err != nil {
		if !apierrors.IsNotFound(err) {
			wctx.SendStatus(newActionResult(
				api.ActionStatusFailed,
				api.ActionResultApplyFailed,
				fmt.Sprintf("failed to get %s %s/%s: %v", kind, namespace, name, err),
			))
			return fmt.Errorf("failed to get %s %s/%s: %w", kind, namespace, name, err)
		}
		// Resource doesn't exist — that's fine, just render
	} else if !isSuspended(existing) {
		// Resource exists but spec.suspend is not true — patch it
		patch := []byte(`{"spec":{"suspend":true}}`)
		if err := k8sClient.Patch(wctx.Context(), existing, client.RawPatch(types.MergePatchType, patch)); err != nil {
			log.Log.Error(err, "Failed to set spec.suspend=true", "kind", kind, "namespace", namespace, "name", name)
		} else {
			log.Log.Info("Set spec.suspend=true", "kind", kind, "namespace", namespace, "name", name)
		}
	}

	// Render manifests
	parsed, err := fluxrenderer.ParseDocumentsWithResources(payload.Data)
	if err != nil {
		wctx.SendStatus(newActionResult(
			api.ActionStatusFailed,
			api.ActionResultApplyFailed,
			fmt.Sprintf("failed to parse input documents: %v", err),
		))
		return fmt.Errorf("failed to parse input documents: %w", err)
	}

	artifactSource, err := lookupArtifactSource(wctx.Context(), k8sClient, parsed)
	if err != nil {
		wctx.SendStatus(newActionResult(
			api.ActionStatusFailed,
			api.ActionResultApplyFailed,
			fmt.Sprintf("failed to lookup artifact source: %v", err),
		))
		return fmt.Errorf("failed to lookup artifact source: %w", err)
	}

	input := fluxrenderer.RenderInput{
		Documents: payload.Data,
		Options: fluxrenderer.RenderOptions{
			ArtifactSource: artifactSource,
		},
	}

	result, err := fluxrenderer.RenderFlux(wctx.Context(), input)
	if err != nil {
		wctx.SendStatus(newActionResult(
			api.ActionStatusFailed,
			api.ActionResultApplyFailed,
			fmt.Sprintf("failed to render Flux resources: %v", err),
		))
		return fmt.Errorf("failed to render Flux resources: %w", err)
	}

	status := newActionResult(
		api.ActionStatusCompleted,
		api.ActionResultApplyCompleted,
		fmt.Sprintf("Rendered successfully at %s (revision: %s)", time.Now().Format(time.RFC3339), result.Revision),
	)
	status.LiveState = result.Manifests
	return wctx.SendStatus(status)
}

// applyNonAuthoritative verifies the Flux resource exists in the cluster, checks for
// spec.suspend warnings, and renders manifests without creating/updating the resource.
func (w *FluxRendererWorker) applyNonAuthoritative(wctx api.BridgeWorkerContext, payload api.BridgeWorkerPayload) error {
	var params KubernetesWorkerParams
	if len(payload.TargetParams) > 0 {
		if err := json.Unmarshal(payload.TargetParams, &params); err != nil {
			wctx.SendStatus(newActionResult(
				api.ActionStatusFailed,
				api.ActionResultApplyFailed,
				fmt.Sprintf("failed to parse target parameters: %v", err),
			))
			return fmt.Errorf("failed to parse target parameters: %w", err)
		}
	}

	k8sClient, err := createFluxRendererK8sClient(params.KubeContext)
	if err != nil {
		wctx.SendStatus(newActionResult(
			api.ActionStatusFailed,
			api.ActionResultApplyFailed,
			fmt.Sprintf("failed to create Kubernetes client: %v", err),
		))
		return fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	// Find the Flux resource from payload
	kind, apiVersion, name, namespace, err := findFluxResource(payload.Data)
	if err != nil {
		wctx.SendStatus(newActionResult(
			api.ActionStatusFailed,
			api.ActionResultApplyFailed,
			fmt.Sprintf("failed to find Flux resource: %v", err),
		))
		return fmt.Errorf("failed to find Flux resource: %w", err)
	}

	// Verify resource exists in cluster
	existing := &unstructured.Unstructured{}
	existing.SetAPIVersion(apiVersion)
	existing.SetKind(kind)
	key := types.NamespacedName{Namespace: namespace, Name: name}
	if err := k8sClient.Get(wctx.Context(), key, existing); err != nil {
		if apierrors.IsNotFound(err) {
			wctx.SendStatus(newActionResult(
				api.ActionStatusFailed,
				api.ActionResultApplyFailed,
				fmt.Sprintf("%s %s/%s not found in cluster (IsAuthoritative=false requires the resource to already exist)", kind, namespace, name),
			))
			return fmt.Errorf("%s %s/%s not found: %w", kind, namespace, name, err)
		}
		wctx.SendStatus(newActionResult(
			api.ActionStatusFailed,
			api.ActionResultApplyFailed,
			fmt.Sprintf("failed to get %s %s/%s: %v", kind, namespace, name, err),
		))
		return fmt.Errorf("failed to get %s %s/%s: %w", kind, namespace, name, err)
	}

	// Check spec.suspend warning
	var errorMessages []string
	if !isSuspended(existing) {
		errorMessages = append(errorMessages, fmt.Sprintf(
			"%s %s/%s: spec.suspend is not true. To avoid conflicts, set spec.suspend to true.",
			kind, namespace, name))
	}

	// Parse the input documents to find the Flux resource
	parsed, err := fluxrenderer.ParseDocumentsWithResources(payload.Data)
	if err != nil {
		wctx.SendStatus(newActionResult(
			api.ActionStatusFailed,
			api.ActionResultApplyFailed,
			fmt.Sprintf("failed to parse input documents: %v", err),
		))
		return fmt.Errorf("failed to parse input documents: %w", err)
	}

	// Find the source reference and look up the artifact
	artifactSource, err := lookupArtifactSource(wctx.Context(), k8sClient, parsed)
	if err != nil {
		wctx.SendStatus(newActionResult(
			api.ActionStatusFailed,
			api.ActionResultApplyFailed,
			fmt.Sprintf("failed to lookup artifact source: %v", err),
		))
		return fmt.Errorf("failed to lookup artifact source: %w", err)
	}

	// Build render input
	input := fluxrenderer.RenderInput{
		Documents: payload.Data,
		Options: fluxrenderer.RenderOptions{
			ArtifactSource: artifactSource,
		},
	}

	// Render the Flux resources
	result, err := fluxrenderer.RenderFlux(wctx.Context(), input)
	if err != nil {
		wctx.SendStatus(newActionResult(
			api.ActionStatusFailed,
			api.ActionResultApplyFailed,
			fmt.Sprintf("failed to render Flux resources: %v", err),
		))
		return fmt.Errorf("failed to render Flux resources: %w", err)
	}

	status := newActionResult(
		api.ActionStatusCompleted,
		api.ActionResultApplyCompleted,
		fmt.Sprintf("Rendered successfully at %s (revision: %s)", time.Now().Format(time.RFC3339), result.Revision),
	)
	status.LiveState = result.Manifests
	status.ErrorMessages = errorMessages
	return wctx.SendStatus(status)
}

func (w *FluxRendererWorker) Destroy(wctx api.BridgeWorkerContext, payload api.BridgeWorkerPayload) error {
	if !isAuthoritative(payload) {
		// Non-authoritative: do not delete the Flux resource
		result := newActionResult(
			api.ActionStatusCompleted,
			api.ActionResultDestroyCompleted,
			fmt.Sprintf("Destroy is a no-op for non-authoritative Flux resource at %s", time.Now().Format(time.RFC3339)),
		)
		return wctx.SendStatus(result)
	}

	// Authoritative: delete the Flux resource via Kubernetes client
	var params KubernetesWorkerParams
	if len(payload.TargetParams) > 0 {
		if err := json.Unmarshal(payload.TargetParams, &params); err != nil {
			wctx.SendStatus(newActionResult(
				api.ActionStatusFailed,
				api.ActionResultDestroyFailed,
				fmt.Sprintf("failed to parse target parameters: %v", err),
			))
			return fmt.Errorf("failed to parse target parameters: %w", err)
		}
	}

	k8sClient, err := createFluxRendererK8sClient(params.KubeContext)
	if err != nil {
		wctx.SendStatus(newActionResult(
			api.ActionStatusFailed,
			api.ActionResultDestroyFailed,
			fmt.Sprintf("failed to create Kubernetes client: %v", err),
		))
		return fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	kind, apiVersion, name, namespace, err := findFluxResource(payload.Data)
	if err != nil {
		wctx.SendStatus(newActionResult(
			api.ActionStatusFailed,
			api.ActionResultDestroyFailed,
			fmt.Sprintf("failed to find Flux resource: %v", err),
		))
		return fmt.Errorf("failed to find Flux resource: %w", err)
	}

	obj := &unstructured.Unstructured{}
	obj.SetAPIVersion(apiVersion)
	obj.SetKind(kind)
	obj.SetName(name)
	obj.SetNamespace(namespace)

	if err := k8sClient.Delete(wctx.Context(), obj); err != nil && !apierrors.IsNotFound(err) {
		wctx.SendStatus(newActionResult(
			api.ActionStatusFailed,
			api.ActionResultDestroyFailed,
			fmt.Sprintf("failed to delete %s %s/%s: %v", kind, namespace, name, err),
		))
		return fmt.Errorf("failed to delete %s %s/%s: %w", kind, namespace, name, err)
	}

	result := newActionResult(
		api.ActionStatusCompleted,
		api.ActionResultDestroyCompleted,
		fmt.Sprintf("Destroyed %s %s/%s at %s", kind, namespace, name, time.Now().Format(time.RFC3339)),
	)
	return wctx.SendStatus(result)
}

func (w *FluxRendererWorker) Refresh(wctx api.BridgeWorkerContext, payload api.BridgeWorkerPayload) error {
	if isAuthoritative(payload) {
		return w.KubernetesBridgeWorker.Refresh(wctx, payload)
	}

	return w.refreshNonAuthoritative(wctx, payload)
}

// refreshNonAuthoritative fetches the Flux resource from the cluster, checks for
// spec.suspend, renders manifests, and compares with previous LiveState to detect drift.
func (w *FluxRendererWorker) refreshNonAuthoritative(wctx api.BridgeWorkerContext, payload api.BridgeWorkerPayload) error {
	var params KubernetesWorkerParams
	if len(payload.TargetParams) > 0 {
		if err := json.Unmarshal(payload.TargetParams, &params); err != nil {
			wctx.SendStatus(newActionResult(
				api.ActionStatusFailed,
				api.ActionResultRefreshFailed,
				fmt.Sprintf("failed to parse target parameters: %v", err),
			))
			return fmt.Errorf("failed to parse target parameters: %w", err)
		}
	}

	k8sClient, err := createFluxRendererK8sClient(params.KubeContext)
	if err != nil {
		wctx.SendStatus(newActionResult(
			api.ActionStatusFailed,
			api.ActionResultRefreshFailed,
			fmt.Sprintf("failed to create Kubernetes client: %v", err),
		))
		return fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	// Find the Flux resource from payload
	kind, apiVersion, name, namespace, err := findFluxResource(payload.Data)
	if err != nil {
		wctx.SendStatus(newActionResult(
			api.ActionStatusFailed,
			api.ActionResultRefreshFailed,
			fmt.Sprintf("failed to find Flux resource: %v", err),
		))
		return fmt.Errorf("failed to find Flux resource: %w", err)
	}

	// Fetch resource from cluster
	existing := &unstructured.Unstructured{}
	existing.SetAPIVersion(apiVersion)
	existing.SetKind(kind)
	key := types.NamespacedName{Namespace: namespace, Name: name}
	if err := k8sClient.Get(wctx.Context(), key, existing); err != nil {
		if apierrors.IsNotFound(err) {
			status := newActionResult(
				api.ActionStatusCompleted,
				api.ActionResultRefreshAndDrifted,
				fmt.Sprintf("%s %s/%s not found in cluster", kind, namespace, name),
			)
			return wctx.SendStatus(status)
		}
		wctx.SendStatus(newActionResult(
			api.ActionStatusFailed,
			api.ActionResultRefreshFailed,
			fmt.Sprintf("failed to get %s %s/%s: %v", kind, namespace, name, err),
		))
		return fmt.Errorf("failed to get %s %s/%s: %w", kind, namespace, name, err)
	}

	// Check spec.suspend warning
	var errorMessages []string
	if !isSuspended(existing) {
		errorMessages = append(errorMessages, fmt.Sprintf(
			"%s %s/%s: spec.suspend is not true. To avoid conflicts, set spec.suspend to true.",
			kind, namespace, name))
	}

	// Parse the input documents to find the Flux resource
	parsed, err := fluxrenderer.ParseDocumentsWithResources(payload.Data)
	if err != nil {
		wctx.SendStatus(newActionResult(
			api.ActionStatusFailed,
			api.ActionResultRefreshFailed,
			fmt.Sprintf("failed to parse input documents: %v", err),
		))
		return fmt.Errorf("failed to parse input documents: %w", err)
	}

	// Lookup artifact source and render
	artifactSource, err := lookupArtifactSource(wctx.Context(), k8sClient, parsed)
	if err != nil {
		wctx.SendStatus(newActionResult(
			api.ActionStatusFailed,
			api.ActionResultRefreshFailed,
			fmt.Sprintf("failed to lookup artifact source: %v", err),
		))
		return fmt.Errorf("failed to lookup artifact source: %w", err)
	}

	input := fluxrenderer.RenderInput{
		Documents: payload.Data,
		Options: fluxrenderer.RenderOptions{
			ArtifactSource: artifactSource,
		},
	}

	renderResult, err := fluxrenderer.RenderFlux(wctx.Context(), input)
	if err != nil {
		wctx.SendStatus(newActionResult(
			api.ActionStatusFailed,
			api.ActionResultRefreshFailed,
			fmt.Sprintf("failed to render Flux resources: %v", err),
		))
		return fmt.Errorf("failed to render Flux resources: %w", err)
	}

	// Compare rendered manifests with previous LiveState to detect drift
	drifted := !bytes.Equal(renderResult.Manifests, payload.LiveState)

	var resultType api.ActionResultType
	var msg string
	if drifted {
		resultType = api.ActionResultRefreshAndDrifted
		msg = fmt.Sprintf("Drift detected at %s (revision: %s)", time.Now().Format(time.RFC3339), renderResult.Revision)
	} else {
		resultType = api.ActionResultRefreshAndNoDrift
		msg = fmt.Sprintf("No drift detected at %s (revision: %s)", time.Now().Format(time.RFC3339), renderResult.Revision)
	}

	status := newActionResult(api.ActionStatusCompleted, resultType, msg)
	status.LiveState = renderResult.Manifests
	status.ErrorMessages = errorMessages
	return wctx.SendStatus(status)
}

func (w *FluxRendererWorker) Import(wctx api.BridgeWorkerContext, payload api.BridgeWorkerPayload) error {
	result := newActionResult(
		api.ActionStatusNone,
		api.ActionResultNone,
		fmt.Sprintf("Import hasn't been implemented yet: %s", time.Now().Format(time.RFC3339)),
	)
	return wctx.SendStatus(result)
}

func (w *FluxRendererWorker) Finalize(wctx api.BridgeWorkerContext, payload api.BridgeWorkerPayload) error {
	return nil
}

func (w *FluxRendererWorker) WatchForApply(wctx api.BridgeWorkerContext, payload api.BridgeWorkerPayload) error {
	// Apply completes inline for both authoritative and non-authoritative paths.
	return nil
}

func (w *FluxRendererWorker) WatchForDestroy(wctx api.BridgeWorkerContext, payload api.BridgeWorkerPayload) error {
	// Destroy completes inline for both authoritative and non-authoritative paths.
	return nil
}

// createFluxRendererK8sClient creates a simple Kubernetes client for looking up source resources.
func createFluxRendererK8sClient(kubeContext string) (client.Client, error) {
	cfg, err := kubernetesConfigFactory(kubeContext)
	if err != nil {
		return nil, fmt.Errorf("failed to get Kubernetes config: %w", err)
	}

	k8sClient, err := client.New(cfg, client.Options{})
	if err != nil {
		return nil, fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	return k8sClient, nil
}

// lookupArtifactSource finds the source reference in the Flux resource and looks up
// the artifact URL and digest from the source controller resource in the cluster.
func lookupArtifactSource(ctx context.Context, k8sClient client.Client, parsed *fluxrenderer.ParsedDocuments) (fluxrenderer.ArtifactSource, error) {
	// Find the HelmRelease or Kustomization
	helmReleases := fluxrenderer.FindAllDocumentsByKindAndAPIVersion(parsed.Documents, fluxrenderer.HelmReleaseKind, fluxrenderer.HelmReleaseAPIVersionPrefix)
	kustomizations := fluxrenderer.FindAllDocumentsByKindAndAPIVersion(parsed.Documents, fluxrenderer.KustomizationKind, fluxrenderer.KustomizationAPIVersionPrefix)

	if len(helmReleases) > 0 {
		return lookupHelmReleaseSource(ctx, k8sClient, helmReleases[0], parsed.HelmCharts)
	}

	if len(kustomizations) > 0 {
		return lookupKustomizationSource(ctx, k8sClient, kustomizations[0])
	}

	return fluxrenderer.ArtifactSource{}, fmt.Errorf("no HelmRelease or Kustomization found in input")
}

// lookupHelmReleaseSource looks up the artifact source for a HelmRelease.
// It handles both spec.chart (creates implicit HelmChart) and spec.chartRef patterns.
func lookupHelmReleaseSource(ctx context.Context, k8sClient client.Client, hr *unstructured.Unstructured, helmCharts map[string]*unstructured.Unstructured) (fluxrenderer.ArtifactSource, error) {
	spec, ok := hr.Object["spec"].(map[string]interface{})
	if !ok {
		return fluxrenderer.ArtifactSource{}, fmt.Errorf("HelmRelease has no spec")
	}

	// Check for chartRef (direct reference to HelmChart or OCIRepository)
	if chartRef, ok := spec["chartRef"].(map[string]interface{}); ok {
		return lookupChartRefSource(ctx, k8sClient, hr, chartRef)
	}

	// Check for spec.chart (creates implicit HelmChart which references HelmRepository)
	if chartSpec, ok := spec["chart"].(map[string]interface{}); ok {
		return lookupImplicitChartSource(ctx, k8sClient, hr, chartSpec, helmCharts)
	}

	return fluxrenderer.ArtifactSource{}, fmt.Errorf("HelmRelease has neither chartRef nor chart spec")
}

// lookupChartRefSource looks up artifact from a chartRef (HelmChart or OCIRepository).
func lookupChartRefSource(ctx context.Context, k8sClient client.Client, hr *unstructured.Unstructured, chartRef map[string]interface{}) (fluxrenderer.ArtifactSource, error) {
	kind, _ := chartRef["kind"].(string)
	name, _ := chartRef["name"].(string)
	namespace := hr.GetNamespace()
	if ns, ok := chartRef["namespace"].(string); ok && ns != "" {
		namespace = ns
	}

	switch kind {
	case "HelmChart":
		return lookupHelmChartArtifact(ctx, k8sClient, namespace, name)
	case "OCIRepository":
		return lookupOCIRepositoryArtifact(ctx, k8sClient, namespace, name)
	default:
		return fluxrenderer.ArtifactSource{}, fmt.Errorf("unsupported chartRef kind: %s", kind)
	}
}

// lookupImplicitChartSource creates and looks up artifact for HelmRelease with spec.chart.
// Like helm-controller, it creates a HelmChart named <namespace>-<name> and waits for it to be ready.
func lookupImplicitChartSource(ctx context.Context, k8sClient client.Client, hr *unstructured.Unstructured, chartSpec map[string]interface{}, helmCharts map[string]*unstructured.Unstructured) (fluxrenderer.ArtifactSource, error) {
	innerSpec, ok := chartSpec["spec"].(map[string]interface{})
	if !ok {
		return fluxrenderer.ArtifactSource{}, fmt.Errorf("chart spec has no inner spec")
	}

	sourceRef, ok := innerSpec["sourceRef"].(map[string]interface{})
	if !ok {
		return fluxrenderer.ArtifactSource{}, fmt.Errorf("chart spec has no sourceRef")
	}

	// The HelmChart is created in the sourceRef namespace with name <hr-namespace>-<hr-name>
	sourceNamespace := hr.GetNamespace()
	if ns, ok := sourceRef["namespace"].(string); ok && ns != "" {
		sourceNamespace = ns
	}

	implicitChartName := hr.GetNamespace() + "-" + hr.GetName()

	// Build the HelmChart resource from the HelmRelease spec.chart
	helmChart := buildHelmChartFromSpec(implicitChartName, sourceNamespace, innerSpec)

	// Create or update the HelmChart in the cluster
	if err := createOrUpdateHelmChart(ctx, k8sClient, helmChart); err != nil {
		return fluxrenderer.ArtifactSource{}, fmt.Errorf("failed to create HelmChart: %w", err)
	}

	// Wait for the HelmChart to have an artifact
	return waitForHelmChartArtifact(ctx, k8sClient, sourceNamespace, implicitChartName)
}

// buildHelmChartFromSpec creates a HelmChart resource from a HelmRelease's spec.chart.spec
func buildHelmChartFromSpec(name, namespace string, chartSpec map[string]interface{}) *unstructured.Unstructured {
	helmChart := &unstructured.Unstructured{}
	helmChart.SetAPIVersion("source.toolkit.fluxcd.io/v1")
	helmChart.SetKind("HelmChart")
	helmChart.SetName(name)
	helmChart.SetNamespace(namespace)

	// Build the spec from the HelmRelease chart spec
	spec := make(map[string]interface{})

	// Copy chart name
	if chart, ok := chartSpec["chart"].(string); ok {
		spec["chart"] = chart
	}

	// Copy version
	if version, ok := chartSpec["version"].(string); ok {
		spec["version"] = version
	}

	// Copy sourceRef
	if sourceRef, ok := chartSpec["sourceRef"].(map[string]interface{}); ok {
		spec["sourceRef"] = sourceRef
	}

	// Copy interval (default to 1h if not specified)
	if interval, ok := chartSpec["interval"].(string); ok {
		spec["interval"] = interval
	} else {
		spec["interval"] = "1h"
	}

	// Copy valuesFiles if present
	if valuesFiles, ok := chartSpec["valuesFiles"].([]interface{}); ok {
		spec["valuesFiles"] = valuesFiles
	}

	// Copy valuesFile if present (single file, deprecated but still supported)
	if valuesFile, ok := chartSpec["valuesFile"].(string); ok {
		spec["valuesFile"] = valuesFile
	}

	// Copy reconcileStrategy if present
	if reconcileStrategy, ok := chartSpec["reconcileStrategy"].(string); ok {
		spec["reconcileStrategy"] = reconcileStrategy
	}

	helmChart.Object["spec"] = spec

	return helmChart
}

// createOrUpdateHelmChart creates the HelmChart if it doesn't exist, or updates it if it does.
func createOrUpdateHelmChart(ctx context.Context, k8sClient client.Client, helmChart *unstructured.Unstructured) error {
	log.FromContext(ctx).Info("Creating HelmChart", "name", helmChart.GetName(), "namespace", helmChart.GetNamespace())

	// Try to create the HelmChart
	err := k8sClient.Create(ctx, helmChart)
	if err == nil {
		return nil
	}

	// If it already exists, update it
	if apierrors.IsAlreadyExists(err) {
		log.FromContext(ctx).Info("HelmChart already exists, updating", "name", helmChart.GetName())

		// Get the existing resource to get its resourceVersion
		existing := &unstructured.Unstructured{}
		existing.SetAPIVersion(helmChart.GetAPIVersion())
		existing.SetKind(helmChart.GetKind())
		key := types.NamespacedName{
			Namespace: helmChart.GetNamespace(),
			Name:      helmChart.GetName(),
		}
		if err := k8sClient.Get(ctx, key, existing); err != nil {
			return fmt.Errorf("failed to get existing HelmChart: %w", err)
		}

		// Set the resourceVersion for update
		helmChart.SetResourceVersion(existing.GetResourceVersion())
		return k8sClient.Update(ctx, helmChart)
	}

	return err
}

// waitForHelmChartArtifact waits for a HelmChart to have an artifact in its status.
func waitForHelmChartArtifact(ctx context.Context, k8sClient client.Client, namespace, name string) (fluxrenderer.ArtifactSource, error) {
	log.FromContext(ctx).Info("Waiting for HelmChart artifact", "name", name, "namespace", namespace)

	var artifactSource fluxrenderer.ArtifactSource

	// Poll every 2 seconds for up to 2 minutes
	err := wait.PollUntilContextTimeout(ctx, 2*time.Second, 2*time.Minute, true, func(ctx context.Context) (bool, error) {
		source, err := lookupHelmChartArtifact(ctx, k8sClient, namespace, name)
		if err != nil {
			// Check if the error is because artifact is not ready yet
			if strings.Contains(err.Error(), "has no artifact") || strings.Contains(err.Error(), "has no status") {
				return false, nil // Keep polling
			}
			return false, err // Return error
		}
		artifactSource = source
		return true, nil // Done
	})

	if err != nil {
		return fluxrenderer.ArtifactSource{}, fmt.Errorf("timeout waiting for HelmChart %s/%s artifact: %w", namespace, name, err)
	}

	log.FromContext(ctx).Info("HelmChart artifact ready", "name", name, "url", artifactSource.URL)
	return artifactSource, nil
}

// lookupKustomizationSource looks up the artifact source for a Kustomization.
func lookupKustomizationSource(ctx context.Context, k8sClient client.Client, ks *unstructured.Unstructured) (fluxrenderer.ArtifactSource, error) {
	spec, ok := ks.Object["spec"].(map[string]interface{})
	if !ok {
		return fluxrenderer.ArtifactSource{}, fmt.Errorf("Kustomization has no spec")
	}

	sourceRef, ok := spec["sourceRef"].(map[string]interface{})
	if !ok {
		return fluxrenderer.ArtifactSource{}, fmt.Errorf("Kustomization has no sourceRef")
	}

	kind, _ := sourceRef["kind"].(string)
	name, _ := sourceRef["name"].(string)
	namespace := ks.GetNamespace()
	if ns, ok := sourceRef["namespace"].(string); ok && ns != "" {
		namespace = ns
	}

	switch kind {
	case "GitRepository":
		return lookupGitRepositoryArtifact(ctx, k8sClient, namespace, name)
	case "OCIRepository":
		return lookupOCIRepositoryArtifact(ctx, k8sClient, namespace, name)
	case "Bucket":
		return lookupBucketArtifact(ctx, k8sClient, namespace, name)
	default:
		return fluxrenderer.ArtifactSource{}, fmt.Errorf("unsupported Kustomization sourceRef kind: %s", kind)
	}
}

// lookupHelmChartArtifact fetches a HelmChart and extracts the artifact URL and digest.
func lookupHelmChartArtifact(ctx context.Context, k8sClient client.Client, namespace, name string) (fluxrenderer.ArtifactSource, error) {
	return lookupSourceArtifact(ctx, k8sClient, "source.toolkit.fluxcd.io/v1", "HelmChart", namespace, name)
}

// lookupGitRepositoryArtifact fetches a GitRepository and extracts the artifact URL and digest.
func lookupGitRepositoryArtifact(ctx context.Context, k8sClient client.Client, namespace, name string) (fluxrenderer.ArtifactSource, error) {
	return lookupSourceArtifact(ctx, k8sClient, "source.toolkit.fluxcd.io/v1", "GitRepository", namespace, name)
}

// lookupOCIRepositoryArtifact fetches an OCIRepository and extracts the artifact URL and digest.
func lookupOCIRepositoryArtifact(ctx context.Context, k8sClient client.Client, namespace, name string) (fluxrenderer.ArtifactSource, error) {
	return lookupSourceArtifact(ctx, k8sClient, "source.toolkit.fluxcd.io/v1", "OCIRepository", namespace, name)
}

// lookupBucketArtifact fetches a Bucket and extracts the artifact URL and digest.
func lookupBucketArtifact(ctx context.Context, k8sClient client.Client, namespace, name string) (fluxrenderer.ArtifactSource, error) {
	return lookupSourceArtifact(ctx, k8sClient, "source.toolkit.fluxcd.io/v1", "Bucket", namespace, name)
}

// lookupSourceArtifact is a generic function to look up any Flux source controller resource
// and extract the artifact URL and digest from its status.
func lookupSourceArtifact(ctx context.Context, k8sClient client.Client, apiVersion, kind, namespace, name string) (fluxrenderer.ArtifactSource, error) {
	// Create an unstructured object for the lookup
	source := &unstructured.Unstructured{}
	source.SetAPIVersion(apiVersion)
	source.SetKind(kind)

	key := types.NamespacedName{
		Namespace: namespace,
		Name:      name,
	}

	if err := k8sClient.Get(ctx, key, source); err != nil {
		return fluxrenderer.ArtifactSource{}, fmt.Errorf("failed to get %s %s/%s: %w", kind, namespace, name, err)
	}

	// Extract artifact from status
	status, ok := source.Object["status"].(map[string]interface{})
	if !ok {
		return fluxrenderer.ArtifactSource{}, fmt.Errorf("%s %s/%s has no status", kind, namespace, name)
	}

	artifact, ok := status["artifact"].(map[string]interface{})
	if !ok {
		return fluxrenderer.ArtifactSource{}, fmt.Errorf("%s %s/%s has no artifact in status (not ready?)", kind, namespace, name)
	}

	url, _ := artifact["url"].(string)
	digest, _ := artifact["digest"].(string)

	if url == "" {
		return fluxrenderer.ArtifactSource{}, fmt.Errorf("%s %s/%s artifact has no URL", kind, namespace, name)
	}

	// For HelmChart, we might need to construct the actual chart URL from the HelmRepository
	// The artifact URL in HelmChart status points to the chart tarball served by source-controller
	if kind == "HelmChart" && strings.Contains(url, "source-controller") {
		// The URL is the internal source-controller URL which is correct for in-cluster access
		// For out-of-cluster, we'd need to use port-forwarding or the actual repository URL
	}

	return fluxrenderer.ArtifactSource{
		URL:    url,
		Digest: digest,
	}, nil
}
