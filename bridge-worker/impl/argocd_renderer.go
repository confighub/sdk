// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package impl

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/confighub/sdk/bridge-worker/api"
	argocdrenderer "github.com/confighub/sdk/bridge-worker/impl/argocd-renderer"
	"github.com/confighub/sdk/workerapi"
)

// ArgoCDRendererWorker renders ArgoCD Application resources to Kubernetes manifests
// by creating the Application in the cluster and calling the ArgoCD API to get
// the rendered manifests.
type ArgoCDRendererWorker struct {
	KubernetesBridgeWorker KubernetesBridgeWorker
}

var _ api.BridgeWorker = (*ArgoCDRendererWorker)(nil)

func (w *ArgoCDRendererWorker) Info(opts api.InfoOptions) api.BridgeWorkerInfo {
	return w.KubernetesBridgeWorker.InfoForToolchainAndProvider(opts, workerapi.ToolchainKubernetesYAML, api.ProviderArgoCDRenderer)
}

func (w *ArgoCDRendererWorker) Apply(wctx api.BridgeWorkerContext, payload api.BridgeWorkerPayload) error {
	// Parse target parameters to get KubeContext
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

	// Create Kubernetes client to manage ArgoCD Application resources
	k8sClient, err := createArgoCDRendererK8sClient(params.KubeContext)
	if err != nil {
		wctx.SendStatus(newActionResult(
			api.ActionStatusFailed,
			api.ActionResultApplyFailed,
			fmt.Sprintf("failed to create Kubernetes client: %v", err),
		))
		return fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	// Read ArgoCD configuration from environment
	config := argocdrenderer.Config{
		ServerAddress: os.Getenv("ARGOCD_SERVER"),
		AuthToken:     os.Getenv("ARGOCD_AUTH_TOKEN"),
		Insecure:      os.Getenv("ARGOCD_INSECURE") != "false",
	}
	if config.ServerAddress == "" {
		config.ServerAddress = "argocd-server.argocd.svc.cluster.local"
	}
	if config.AuthToken == "" {
		wctx.SendStatus(newActionResult(
			api.ActionStatusFailed,
			api.ActionResultApplyFailed,
			"ARGOCD_AUTH_TOKEN environment variable is required",
		))
		return fmt.Errorf("ARGOCD_AUTH_TOKEN environment variable is required")
	}

	// Render the ArgoCD Application
	result, err := argocdrenderer.RenderArgoCD(wctx.Context(), payload.Data, k8sClient, config)
	if err != nil {
		wctx.SendStatus(newActionResult(
			api.ActionStatusFailed,
			api.ActionResultApplyFailed,
			fmt.Sprintf("failed to render ArgoCD Application: %v", err),
		))
		return fmt.Errorf("failed to render ArgoCD Application: %w", err)
	}

	// Return the rendered manifests in LiveState
	status := newActionResult(
		api.ActionStatusCompleted,
		api.ActionResultApplyCompleted,
		fmt.Sprintf("Rendered successfully at %s (revision: %s, sourceType: %s)", time.Now().Format(time.RFC3339), result.Revision, result.SourceType),
	)
	status.LiveState = result.Manifests
	return wctx.SendStatus(status)
}

func (w *ArgoCDRendererWorker) Destroy(wctx api.BridgeWorkerContext, payload api.BridgeWorkerPayload) error {
	result := newActionResult(
		api.ActionStatusNone,
		api.ActionResultNone,
		fmt.Sprintf("Destroy hasn't been implemented yet: %s", time.Now().Format(time.RFC3339)),
	)
	return wctx.SendStatus(result)
}

func (w *ArgoCDRendererWorker) Refresh(wctx api.BridgeWorkerContext, payload api.BridgeWorkerPayload) error {
	result := newActionResult(
		api.ActionStatusNone,
		api.ActionResultNone,
		fmt.Sprintf("Refresh hasn't been implemented yet: %s", time.Now().Format(time.RFC3339)),
	)
	return wctx.SendStatus(result)
}

func (w *ArgoCDRendererWorker) Import(wctx api.BridgeWorkerContext, payload api.BridgeWorkerPayload) error {
	result := newActionResult(
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
	// ArgoCDRenderer doesn't apply to a cluster, so there's nothing to watch
	return nil
}

func (w *ArgoCDRendererWorker) WatchForDestroy(wctx api.BridgeWorkerContext, payload api.BridgeWorkerPayload) error {
	// ArgoCDRenderer doesn't apply to a cluster, so there's nothing to watch
	return nil
}

// createArgoCDRendererK8sClient creates a Kubernetes client for managing ArgoCD Application resources.
func createArgoCDRendererK8sClient(kubeContext string) (client.Client, error) {
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
