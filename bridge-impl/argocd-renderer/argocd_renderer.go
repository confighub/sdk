// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package argocdrenderer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/cenkalti/backoff/v5"
	"github.com/cockroachdb/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	sigsyaml "sigs.k8s.io/yaml"

	"github.com/confighub/sdk/bridge-impl/common"
	"github.com/confighub/sdk/bridge-impl/kubernetes"
	funcapi "github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/worker/api"
	"github.com/confighub/sdk/core/worker/lib"
	"github.com/confighub/sdk/core/workerapi"
)

// ArgoCDRendererWorker renders ArgoCD Application resources to Kubernetes manifests
// by creating the Application in the cluster and calling the ArgoCD API to get
// the rendered manifests.
//
// When IsAuthoritative=true, Apply and Destroy of the Application resource
// are delegated to KubernetesBridgeWorker (SSA). WatchForApply then renders manifests
// via the ArgoCD API and returns them as LiveState.
//
// When IsAuthoritative=false (default), the Application must already exist in the cluster.
// The bridge only reads rendered manifests without modifying the Application.
type ArgoCDRendererWorker struct {
	KubernetesBridgeWorker kubernetes.KubernetesBridgeWorker
}

func NewArgoCDRendererWorker() *ArgoCDRendererWorker {
	// FIXME: Note that this doesn't call NewKubernetesBridgeWorker(), because ArgoCDRendererWorker
	// inlines KubernetesBridgeWorker rather than storing a pointer.
	return &ArgoCDRendererWorker{}
}

var _ api.BridgeWorker = (*ArgoCDRendererWorker)(nil)
var _ api.WatchableWorker = (*ArgoCDRendererWorker)(nil)

func (w *ArgoCDRendererWorker) ID() api.BridgeWorkerID {
	return api.BridgeWorkerID{
		ProviderType:   api.ProviderArgoCDRenderer,
		ToolchainTypes: []workerapi.ToolchainType{workerapi.ToolchainKubernetesYAML},
	}
}

func (w *ArgoCDRendererWorker) Info(opts api.InfoOptions) api.BridgeWorkerInfo {
	info := w.KubernetesBridgeWorker.InfoForToolchainAndProvider(opts, workerapi.ToolchainKubernetesYAML, api.ProviderArgoCDRenderer)
	for i := range info.SupportedConfigTypes {
		info.SupportedConfigTypes[i].Options = append(info.SupportedConfigTypes[i].Options, api.BridgeOption{
			Name:        "IsAuthoritative",
			Description: "When true, the bridge creates/updates/deletes the ArgoCD Application resource. When false (default), the Application must already exist and the bridge only reads rendered manifests from it.",
			Required:    false,
			DataType:    funcapi.DataTypeBool,
		})
	}
	return info
}

// isAuthoritative returns whether the bridge should manage the resource lifecycle.
// Defaults to false if not set.
func isAuthoritative(payload api.BridgeWorkerPayload) bool {
	if payload.DryRun {
		return false
	}
	if v, ok := payload.TargetOptions["IsAuthoritative"]; ok {
		return v == "true"
	}
	return false
}

// getArgoCDConfig reads ArgoCD configuration from environment variables.
func getArgoCDConfig() Config {
	config := Config{
		ServerAddress: os.Getenv("ARGOCD_SERVER"),
		AuthToken:     os.Getenv("ARGOCD_AUTH_TOKEN"),
		Insecure:      os.Getenv("ARGOCD_INSECURE") != "false",
	}
	if config.ServerAddress == "" {
		config.ServerAddress = "argocd-server.argocd.svc.cluster.local"
	}
	return config
}

// prepareApplicationPayload parses the Application YAML from the payload,
// disables autosync, adds refresh/hydrate annotations to trigger ArgoCD to
// re-fetch sources from git, re-serializes, and returns a modified payload.
func prepareApplicationPayload(payload api.BridgeWorkerPayload) (api.BridgeWorkerPayload, error) {
	app, err := ParseApplication(payload.Data)
	if err != nil {
		return payload, fmt.Errorf("failed to parse Application: %w", err)
	}

	DisableAutoSync(app)
	SetRefreshAnnotations(app)

	jsonBytes, err := app.MarshalJSON()
	if err != nil {
		return payload, fmt.Errorf("failed to marshal Application to JSON: %w", err)
	}
	yamlBytes, err := sigsyaml.JSONToYAML(jsonBytes)
	if err != nil {
		return payload, fmt.Errorf("failed to convert Application to YAML: %w", err)
	}

	modified := payload
	modified.Data = yamlBytes
	return modified, nil
}

func (w *ArgoCDRendererWorker) Apply(wctx api.BridgeWorkerContext, payload api.BridgeWorkerPayload) error {
	if !isAuthoritative(payload) {
		return w.applyNonAuthoritative(wctx, payload)
	}

	// Authoritative: disable autosync, add refresh annotations, and delegate to KubernetesBridgeWorker for SSA apply
	modifiedPayload, err := prepareApplicationPayload(payload)
	if err != nil {
		wctx.SendStatus(common.NewActionResult(
			api.ActionStatusFailed,
			api.ActionResultApplyFailed,
			fmt.Sprintf("failed to prepare Application: %v", err),
		))
		return fmt.Errorf("failed to prepare Application: %w", err)
	}

	return w.KubernetesBridgeWorker.Apply(wctx, modifiedPayload)
}

// applyNonAuthoritative verifies the Application exists, checks for autosync warnings,
// and renders manifests via the ArgoCD API without creating/updating the Application.
func (w *ArgoCDRendererWorker) applyNonAuthoritative(wctx api.BridgeWorkerContext, payload api.BridgeWorkerPayload) error {
	var params kubernetes.KubernetesWorkerParams
	if len(payload.TargetParams) > 0 {
		if err := json.Unmarshal(payload.TargetParams, &params); err != nil {
			wctx.SendStatus(common.NewActionResult(
				api.ActionStatusFailed,
				api.ActionResultApplyFailed,
				fmt.Sprintf("failed to parse target parameters: %v", err),
			))
			return fmt.Errorf("failed to parse target parameters: %w", err)
		}
	}

	k8sClient, err := createArgoCDRendererK8sClient(params.KubeContext)
	if err != nil {
		wctx.SendStatus(common.NewActionResult(
			api.ActionStatusFailed,
			api.ActionResultApplyFailed,
			fmt.Sprintf("failed to create Kubernetes client: %v", err),
		))
		return fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	config := getArgoCDConfig()
	if config.AuthToken == "" {
		wctx.SendStatus(common.NewActionResult(
			api.ActionStatusFailed,
			api.ActionResultApplyFailed,
			"ARGOCD_AUTH_TOKEN environment variable is required",
		))
		return fmt.Errorf("ARGOCD_AUTH_TOKEN environment variable is required")
	}

	app, err := ParseApplication(payload.Data)
	if err != nil {
		wctx.SendStatus(common.NewActionResult(
			api.ActionStatusFailed,
			api.ActionResultApplyFailed,
			fmt.Sprintf("failed to parse Application: %v", err),
		))
		return fmt.Errorf("failed to parse Application: %w", err)
	}

	appName := app.GetName()
	appNamespace := app.GetNamespace()
	if appNamespace == "" {
		appNamespace = "argocd"
	}

	// Verify Application exists in the cluster
	existing := &unstructured.Unstructured{}
	existing.SetAPIVersion(app.GetAPIVersion())
	existing.SetKind(app.GetKind())
	key := types.NamespacedName{Namespace: appNamespace, Name: appName}
	if err := k8sClient.Get(wctx.Context(), key, existing); err != nil {
		if apierrors.IsNotFound(err) {
			wctx.SendStatus(common.NewActionResult(
				api.ActionStatusFailed,
				api.ActionResultApplyFailed,
				fmt.Sprintf("Application %s/%s not found in cluster (IsAuthoritative=false requires the Application to already exist)", appNamespace, appName),
			))
			return fmt.Errorf("Application %s/%s not found: %w", appNamespace, appName, err)
		}
		wctx.SendStatus(common.NewActionResult(
			api.ActionStatusFailed,
			api.ActionResultApplyFailed,
			fmt.Sprintf("failed to get Application %s/%s: %v", appNamespace, appName, err),
		))
		return fmt.Errorf("failed to get Application %s/%s: %w", appNamespace, appName, err)
	}

	// Autosync must be disabled so that ConfigHub owns the rendered output.
	if HasAutoSync(existing) {
		msg := fmt.Sprintf(
			"Application %s/%s: autosync is enabled. To avoid conflicts, set spec.syncPolicy.automated to null or remove it, or set the bridge option IsAuthoritative=true on the Target to let the bridge update the Application automatically (including removing autosync).",
			appNamespace, appName)
		wctx.SendStatus(common.NewActionResult(
			api.ActionStatusFailed,
			api.ActionResultApplyFailed,
			msg,
		))
		return fmt.Errorf("%s", msg)
	}

	// Patch refresh/hydrate annotations to trigger ArgoCD to re-fetch sources from git
	if err := PatchRefreshAnnotations(wctx.Context(), k8sClient, appName, appNamespace); err != nil {
		wctx.SendStatus(common.NewActionResult(
			api.ActionStatusFailed,
			api.ActionResultApplyFailed,
			fmt.Sprintf("failed to patch refresh annotations on Application %s/%s: %v", appNamespace, appName, err),
		))
		return fmt.Errorf("failed to patch refresh annotations on Application %s/%s: %w", appNamespace, appName, err)
	}

	// Render manifests via ArgoCD API (Application already exists)
	result, err := RenderExistingArgoCD(wctx.Context(), k8sClient, appName, appNamespace, config)
	if err != nil {
		wctx.SendStatus(common.NewActionResult(
			api.ActionStatusFailed,
			api.ActionResultApplyFailed,
			fmt.Sprintf("failed to render ArgoCD Application: %v", err),
		))
		return fmt.Errorf("failed to render ArgoCD Application: %w", err)
	}

	status := common.NewActionResult(
		api.ActionStatusCompleted,
		api.ActionResultApplyCompleted,
		fmt.Sprintf("Rendered successfully at %s (revision: %s, sourceType: %s)", time.Now().Format(time.RFC3339), result.Revision, result.SourceType),
	)
	status.LiveState = result.Manifests
	return wctx.SendStatus(status)
}

func (w *ArgoCDRendererWorker) Destroy(wctx api.BridgeWorkerContext, payload api.BridgeWorkerPayload) error {
	if !isAuthoritative(payload) {
		// Non-authoritative: do not delete the Application
		result := common.NewActionResult(
			api.ActionStatusCompleted,
			api.ActionResultDestroyCompleted,
			fmt.Sprintf("Destroy is a no-op for non-authoritative Application at %s", time.Now().Format(time.RFC3339)),
		)
		return wctx.SendStatus(result)
	}

	// Authoritative: delegate to KubernetesBridgeWorker for SSA destroy
	return w.KubernetesBridgeWorker.Destroy(wctx, payload)
}

func (w *ArgoCDRendererWorker) Refresh(wctx api.BridgeWorkerContext, payload api.BridgeWorkerPayload) error {
	if isAuthoritative(payload) {
		return w.KubernetesBridgeWorker.Refresh(wctx, payload)
	}

	return w.refreshNonAuthoritative(wctx, payload)
}

// refreshNonAuthoritative fetches the Application from the cluster, checks for autosync,
// renders manifests, and compares with previous LiveState to detect drift.
func (w *ArgoCDRendererWorker) refreshNonAuthoritative(wctx api.BridgeWorkerContext, payload api.BridgeWorkerPayload) error {
	var params kubernetes.KubernetesWorkerParams
	if len(payload.TargetParams) > 0 {
		if err := json.Unmarshal(payload.TargetParams, &params); err != nil {
			wctx.SendStatus(common.NewActionResult(
				api.ActionStatusFailed,
				api.ActionResultRefreshFailed,
				fmt.Sprintf("failed to parse target parameters: %v", err),
			))
			return fmt.Errorf("failed to parse target parameters: %w", err)
		}
	}

	k8sClient, err := createArgoCDRendererK8sClient(params.KubeContext)
	if err != nil {
		wctx.SendStatus(common.NewActionResult(
			api.ActionStatusFailed,
			api.ActionResultRefreshFailed,
			fmt.Sprintf("failed to create Kubernetes client: %v", err),
		))
		return fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	config := getArgoCDConfig()
	if config.AuthToken == "" {
		wctx.SendStatus(common.NewActionResult(
			api.ActionStatusFailed,
			api.ActionResultRefreshFailed,
			"ARGOCD_AUTH_TOKEN environment variable is required",
		))
		return fmt.Errorf("ARGOCD_AUTH_TOKEN environment variable is required")
	}

	app, err := ParseApplication(payload.Data)
	if err != nil {
		wctx.SendStatus(common.NewActionResult(
			api.ActionStatusFailed,
			api.ActionResultRefreshFailed,
			fmt.Sprintf("failed to parse Application: %v", err),
		))
		return fmt.Errorf("failed to parse Application: %w", err)
	}

	appName := app.GetName()
	appNamespace := app.GetNamespace()
	if appNamespace == "" {
		appNamespace = "argocd"
	}

	// Fetch Application from cluster
	existing := &unstructured.Unstructured{}
	existing.SetAPIVersion(app.GetAPIVersion())
	existing.SetKind(app.GetKind())
	key := types.NamespacedName{Namespace: appNamespace, Name: appName}
	if err := k8sClient.Get(wctx.Context(), key, existing); err != nil {
		if apierrors.IsNotFound(err) {
			status := common.NewActionResult(
				api.ActionStatusCompleted,
				api.ActionResultRefreshAndDrifted,
				fmt.Sprintf("Application %s/%s not found in cluster", appNamespace, appName),
			)
			return wctx.SendStatus(status)
		}
		wctx.SendStatus(common.NewActionResult(
			api.ActionStatusFailed,
			api.ActionResultRefreshFailed,
			fmt.Sprintf("failed to get Application %s/%s: %v", appNamespace, appName, err),
		))
		return fmt.Errorf("failed to get Application %s/%s: %w", appNamespace, appName, err)
	}

	// Autosync must be disabled so that ConfigHub owns the rendered output.
	if HasAutoSync(existing) {
		msg := fmt.Sprintf(
			"Application %s/%s: autosync is enabled. To avoid conflicts, set spec.syncPolicy.automated to null or remove it, or set the bridge option IsAuthoritative=true on the Target to let the bridge update the Application automatically (including removing autosync).",
			appNamespace, appName)
		wctx.SendStatus(common.NewActionResult(
			api.ActionStatusFailed,
			api.ActionResultRefreshFailed,
			msg,
		))
		return fmt.Errorf("%s", msg)
	}

	// Render manifests via ArgoCD API
	renderResult, err := RenderExistingArgoCD(wctx.Context(), k8sClient, appName, appNamespace, config)
	if err != nil {
		wctx.SendStatus(common.NewActionResult(
			api.ActionStatusFailed,
			api.ActionResultRefreshFailed,
			fmt.Sprintf("failed to render ArgoCD Application: %v", err),
		))
		return fmt.Errorf("failed to render ArgoCD Application: %w", err)
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

	status := common.NewActionResult(api.ActionStatusCompleted, resultType, msg)
	status.LiveState = renderResult.Manifests
	return wctx.SendStatus(status)
}

func (w *ArgoCDRendererWorker) Import(wctx api.BridgeWorkerContext, payload api.BridgeWorkerPayload) error {
	result := common.NewActionResult(
		api.ActionStatusNone,
		api.ActionResultNone,
		fmt.Sprintf("Import hasn't been implemented yet: %s", time.Now().Format(time.RFC3339)),
	)
	return wctx.SendStatus(result)
}

func (w *ArgoCDRendererWorker) Finalize(wctx api.BridgeWorkerContext, payload api.BridgeWorkerPayload) error {
	return nil
}

func (w *ArgoCDRendererWorker) WatchForApply(wctx api.BridgeWorkerContext, payload api.BridgeWorkerPayload) error {
	if !isAuthoritative(payload) {
		// Non-authoritative Apply already sends ApplyCompleted directly
		return nil
	}

	// Authoritative: wait for the Application resource to be ready,
	// then render manifests via ArgoCD API
	return w.watchForApplyAuthoritative(wctx, payload)
}

// watchForApplyAuthoritative waits for the Application resource to be ready in the cluster,
// then calls the ArgoCD API to render manifests and sends ApplyCompleted with LiveState.
func (w *ArgoCDRendererWorker) watchForApplyAuthoritative(wctx api.BridgeWorkerContext, payload api.BridgeWorkerPayload) error {
	workerParams, _, err := kubernetes.ParseTargetParams(payload)
	if err != nil {
		return backoff.Permanent(lib.SafeSendStatus(wctx, common.NewActionResult(
			api.ActionStatusFailed,
			api.ActionResultApplyWaitFailed,
			err.Error(),
		), err))
	}

	applier, err := w.KubernetesBridgeWorker.GetOrCreateApplier(payload)
	if err != nil {
		return lib.SafeSendStatus(wctx, common.NewActionResult(
			api.ActionStatusFailed,
			api.ActionResultApplyWaitFailed,
			err.Error(),
		), err)
	}

	// Parse objects from payload (already prepared with autosync disabled and refresh annotations by Apply)
	objects, err := kubernetes.ParseObjects(payload.Data)
	if err != nil {
		return backoff.Permanent(lib.SafeSendStatus(wctx, common.NewActionResult(
			api.ActionStatusFailed,
			api.ActionResultApplyWaitFailed,
			err.Error(),
		), err))
	}

	if err := wctx.SendStatus(common.NewActionResult(
		api.ActionStatusProgressing,
		api.ActionResultNone,
		"Waiting for Application resource to be ready...",
	)); err != nil {
		return err
	}

	// Parse timeout from worker params
	var timeout time.Duration
	if workerParams.WaitTimeout != "" {
		if t, err := time.ParseDuration(workerParams.WaitTimeout); err != nil {
			log.Log.Error(err, "Invalid wait timeout format, using default", "timeout", workerParams.WaitTimeout)
		} else {
			timeout = t
		}
	}

	// Wait for the Application resource to be ready
	waitResult := applier.WaitForApply(wctx, objects, timeout)
	if waitResult.Error != nil {
		if errors.Is(waitResult.Error, context.Canceled) {
			log.Log.Info("Apply wait cancelled")
			return nil
		}
		if errors.Is(waitResult.Error, kubernetes.ErrOperationInterrupted) {
			return nil
		}
		select {
		case <-wctx.Context().Done():
			log.Log.Info("Apply operation was cancelled/overridden, not sending failure status")
			return nil
		default:
		}

		log.Log.Error(waitResult.Error, "Failed to wait for Application resource")
		if errors.Is(waitResult.Error, context.DeadlineExceeded) {
			b := kubernetes.CreateRetryBackoff(workerParams)
			nextBackoff := b.NextBackOff()
			lib.SafeSendStatus(wctx, common.NewActionResult(
				api.ActionStatusProgressing,
				api.ActionResultNone,
				fmt.Sprintf("Application not ready, retrying in %v: %v", nextBackoff, waitResult.Error),
			), waitResult.Error)
			return backoff.RetryAfter(int(nextBackoff.Seconds()))
		}
		return lib.SafeSendStatus(wctx, common.NewActionResult(
			api.ActionStatusFailed,
			api.ActionResultApplyWaitFailed,
			waitResult.Error.Error(),
		), waitResult.Error)
	}

	// Application is ready — now render manifests via ArgoCD API
	config := getArgoCDConfig()
	if config.AuthToken == "" {
		return backoff.Permanent(lib.SafeSendStatus(wctx, common.NewActionResult(
			api.ActionStatusFailed,
			api.ActionResultApplyWaitFailed,
			"ARGOCD_AUTH_TOKEN environment variable is required",
		), fmt.Errorf("ARGOCD_AUTH_TOKEN environment variable is required")))
	}

	k8sClient, err := createArgoCDRendererK8sClient(workerParams.KubeContext)
	if err != nil {
		return backoff.Permanent(lib.SafeSendStatus(wctx, common.NewActionResult(
			api.ActionStatusFailed,
			api.ActionResultApplyWaitFailed,
			fmt.Sprintf("failed to create Kubernetes client: %v", err),
		), err))
	}

	app, err := ParseApplication(payload.Data)
	if err != nil {
		return backoff.Permanent(lib.SafeSendStatus(wctx, common.NewActionResult(
			api.ActionStatusFailed,
			api.ActionResultApplyWaitFailed,
			fmt.Sprintf("failed to parse Application: %v", err),
		), err))
	}

	appName := app.GetName()
	appNamespace := app.GetNamespace()
	if appNamespace == "" {
		appNamespace = "argocd"
	}

	if err := wctx.SendStatus(common.NewActionResult(
		api.ActionStatusProgressing,
		api.ActionResultNone,
		"Rendering manifests via ArgoCD API...",
	)); err != nil {
		return err
	}

	renderResult, err := RenderExistingArgoCD(wctx.Context(), k8sClient, appName, appNamespace, config)
	if err != nil {
		return lib.SafeSendStatus(wctx, common.NewActionResult(
			api.ActionStatusFailed,
			api.ActionResultApplyWaitFailed,
			fmt.Sprintf("failed to render ArgoCD Application: %v", err),
		), err)
	}

	// Check if operation was cancelled while rendering
	select {
	case <-wctx.Context().Done():
		log.Log.Info("Apply operation was cancelled/overridden, skipping completion status")
		return nil
	default:
	}

	status := common.NewActionResult(
		api.ActionStatusCompleted,
		api.ActionResultApplyCompleted,
		fmt.Sprintf("Rendered successfully at %s (revision: %s, sourceType: %s)", time.Now().Format(time.RFC3339), renderResult.Revision, renderResult.SourceType),
	)
	status.LiveState = renderResult.Manifests

	wctx.SendStatus(status)
	return nil
}

func (w *ArgoCDRendererWorker) WatchForDestroy(wctx api.BridgeWorkerContext, payload api.BridgeWorkerPayload) error {
	if !isAuthoritative(payload) {
		return nil
	}

	// Authoritative: delegate to KubernetesBridgeWorker
	return w.KubernetesBridgeWorker.WatchForDestroy(wctx, payload)
}

// createArgoCDRendererK8sClient creates a Kubernetes client for managing ArgoCD Application resources.
func createArgoCDRendererK8sClient(kubeContext string) (client.Client, error) {
	cfg, err := kubernetes.KubernetesConfigFactory(kubeContext)
	if err != nil {
		return nil, fmt.Errorf("failed to get Kubernetes config: %w", err)
	}

	k8sClient, err := client.New(cfg, client.Options{})
	if err != nil {
		return nil, fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	return k8sClient, nil
}
