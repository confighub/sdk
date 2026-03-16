// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package kubernetes

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/cenkalti/backoff/v5"
	"github.com/cockroachdb/errors"
	ssautil "github.com/fluxcd/pkg/ssa/utils"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/cli-utils/pkg/inventory"
	"sigs.k8s.io/cli-utils/pkg/object"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/confighub/sdk/bridge-impl/common"
	"github.com/confighub/sdk/worker/api"
	"github.com/confighub/sdk/worker/lib"
	"github.com/confighub/sdk/configkit/cubkit"
	"github.com/confighub/sdk/configkit/k8skit"
	"github.com/confighub/sdk/configkit/yamlkit"
	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/confighub/sdk/third_party/gaby"
	"github.com/confighub/sdk/workerapi"
)

type KubernetesBridgeWorker struct {
	cfg              *rest.Config
	applier          K8sApplier // Deprecated: for backward compatibility, will be removed
	applierType      ApplierName
	resourceProvider *k8skit.K8sResourceProviderType
	// Removed retryCount - using stateless backoff library instead
}

var _ api.BridgeWorker = (*KubernetesBridgeWorker)(nil)
var _ api.WatchableWorker = (*KubernetesBridgeWorker)(nil)

// GetApplierType returns the applier type used by this bridge worker.
func (w *KubernetesBridgeWorker) GetApplierType() ApplierName { return w.applierType }

// SetApplierType sets the applier type used by this bridge worker.
func (w *KubernetesBridgeWorker) SetApplierType(t ApplierName) { w.applierType = t }

// GetResourceProvider returns the resource provider used by this bridge worker.
func (w *KubernetesBridgeWorker) GetResourceProvider() *k8skit.K8sResourceProviderType {
	return w.resourceProvider
}

// CreateRetryBackoff creates a standard exponential backoff policy for retrying operations
// It derives retry parameters from the WaitTimeout to ensure proper scaling:
// - For short timeouts (< 2m): quick retries starting at 5s
// - For medium timeouts (2m-10m): standard retries starting at 10s
// - For long timeouts (> 10m): slower retries starting at 30s
func CreateRetryBackoff(params KubernetesWorkerParams) *backoff.ExponentialBackOff {
	b := backoff.NewExponentialBackOff()

	// Parse the wait timeout to derive backoff parameters
	var waitTimeout time.Duration = 2 * time.Minute // default
	if params.WaitTimeout != "" {
		if t, err := time.ParseDuration(params.WaitTimeout); err == nil {
			waitTimeout = t
		}
	}

	// Derive backoff parameters based on wait timeout
	// Formula: Scale initial interval and max interval based on timeout magnitude
	switch {
	case waitTimeout < 2*time.Minute:
		// Short timeout: quick retries (5s, 10s, 20s, 40s...)
		b.InitialInterval = 5 * time.Second
		b.MaxInterval = 5 * time.Minute
		b.Multiplier = 2.0

	case waitTimeout <= 10*time.Minute:
		// Medium timeout: standard retries (10s, 20s, 40s, 80s...)
		b.InitialInterval = 10 * time.Second
		b.MaxInterval = 5 * time.Minute
		b.Multiplier = 2.0

	default:
		// Long timeout: slower retries (30s, 60s, 120s, 240s...)
		b.InitialInterval = 30 * time.Second
		b.MaxInterval = 5 * time.Minute
		b.Multiplier = 2.0
	}

	// Allow override with explicit retry configuration if provided
	if params.RetryInitialInterval != "" {
		if duration, err := time.ParseDuration(params.RetryInitialInterval); err == nil {
			b.InitialInterval = duration
		}
	}

	if params.RetryMultiplier > 0 {
		b.Multiplier = params.RetryMultiplier
	}

	if params.RetryMaxInterval != "" {
		if duration, err := time.ParseDuration(params.RetryMaxInterval); err == nil {
			b.MaxInterval = duration
		}
	}

	b.RandomizationFactor = 0.1 // Small jitter to prevent thundering herd
	// Note: In backoff/v5, ExponentialBackOff.NextBackOff() never returns backoff.Stop
	// so retries are already infinite. MaxInterval caps the growth at 5 minutes.
	b.Reset()
	return b
}

// NewKubernetesBridgeWorker creates a new KubernetesBridgeWorker with CLI Utils SSA as default
func NewKubernetesBridgeWorker() *KubernetesBridgeWorker {
	defaultApplier := CLIUtilsSSA

	// Check for deprecated environment variable
	if oldEnv := os.Getenv("CONFIGHUB_USE_CLIUTILS_APPLIER"); oldEnv != "" {
		log.Log.Info("⚠️ CONFIGHUB_USE_CLIUTILS_APPLIER is deprecated and will be ignored. CLIUtils applier is now the default. Use USE_LEGACY_FLUX_APPLIER=true to opt-out if needed.")
	}

	// Check for legacy FluxSSA opt-in
	if os.Getenv("USE_LEGACY_FLUX_APPLIER") == "true" ||
		os.Getenv("USE_LEGACY_FLUX_APPLIER") == "1" {
		defaultApplier = FluxSSA
		log.Log.Info("⚠️ Using legacy FluxSSA applier via USE_LEGACY_FLUX_APPLIER environment variable")
	}

	return &KubernetesBridgeWorker{
		applierType:      defaultApplier, // Default to CLI Utils SSA for inventory support
		resourceProvider: k8skit.NewK8sResourceProvider(),
	}
}

func generateLegacyKubernetesTargetName(workerSlug string, suffix string) string {
	// provider is Kubernetes
	// toolchain is Kubernetes/YAML

	// Legacy names use a short prefix

	name := "k8s-" + workerSlug
	if suffix != "" {
		name += "-" + cubkit.CubNormalizeName(suffix)
	}
	return name
}

// getResourcesFromImportSource determines the appropriate resource fetching method
// based on the provided ExtraParams and returns the resources
func (w *KubernetesBridgeWorker) getResourcesFromImportSource(k8sclient KubernetesClient, importRequest *goclientnew.ImportRequest) ([]*unstructured.Unstructured, error) {
	if importRequest != nil {
		config := NewImportConfigFromRequest(importRequest)
		return GetResourcesWithConfig(k8sclient, config, w.cfg)
	}

	// Fall back to default behavior (get all cluster resources)
	config := &ImportConfig{IncludeSystem: false, IncludeCustom: true, IncludeCluster: true, Filters: []goclientnew.ImportFilter{}}
	return GetResourcesWithConfig(k8sclient, config, w.cfg)
}

func (w *KubernetesBridgeWorker) ID() api.BridgeWorkerID {
	return api.BridgeWorkerID{
		ProviderType:   api.ProviderKubernetes,
		ToolchainTypes: []workerapi.ToolchainType{workerapi.ToolchainKubernetesYAML},
	}
}

func (w *KubernetesBridgeWorker) Info(opts api.InfoOptions) api.BridgeWorkerInfo {
	return w.InfoForToolchainAndProvider(opts, workerapi.ToolchainKubernetesYAML, api.ProviderKubernetes)
}

// This supports ToolchainTypes and ProviderTypes that generate and apply Kubernetes resources.
func (w *KubernetesBridgeWorker) InfoForToolchainAndProvider(opts api.InfoOptions, toolchain workerapi.ToolchainType, provider api.ProviderType) api.BridgeWorkerInfo {
	// Get available contexts
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	k8sCmdConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		loadingRules,
		&clientcmd.ConfigOverrides{},
	)
	cfg, err := k8sCmdConfig.ClientConfig()
	// if we can get the config, use it
	// otherwise, we'll use the in-cluster config
	if err == nil {
		w.cfg = cfg
	}

	kubeConfig, err := k8sCmdConfig.RawConfig()
	if err != nil {
		return api.BridgeWorkerInfo{}
	}

	// Check if we're running inside a Kubernetes cluster
	// When running inside a cluster, the in-cluster config will be used
	// and we don't need to list available contexts
	if cfg, err := rest.InClusterConfig(); err == nil {
		w.cfg = cfg
		log.Log.Info("Running inside Kubernetes cluster, using in-cluster configuration")

		defaultName := false
		targetName := os.Getenv("CONFIGHUB_IN_CLUSTER_TARGET_NAME")
		if targetName == "" {
			// TODO: Deprecated. Remove this eventually.
			targetName = os.Getenv("IN_CLUSTER_TARGET_NAME")
		}

		// Ensure target is unique
		if targetName == "" || toolchain != workerapi.ToolchainKubernetesYAML {
			defaultName = true
			targetName = api.GenerateTargetName(opts.WorkerSlug, provider, toolchain, "cluster")
		}

		targets := []api.Target{
			{
				BridgeHandle: "cluster",
				Name:         targetName,
				Params: KubernetesWorkerParams{
					WaitTimeout: LargeWaitTimeout.String(),
				}.ToMap(),
			},
		}
		// Add legacy target for backward compatibility
		if defaultName && toolchain == workerapi.ToolchainKubernetesYAML {
			legacyTargetName := generateLegacyKubernetesTargetName(opts.WorkerSlug, "")
			targets = append(targets, api.Target{
				BridgeHandle: "cluster",
				Name:         legacyTargetName,
				Params: KubernetesWorkerParams{
					WaitTimeout: LargeWaitTimeout.String(),
				}.ToMap(),
			})
		}

		return api.BridgeWorkerInfo{
			SupportedConfigTypes: []*api.SupportedConfigType{
				{
					ConfigTypeSignature: api.ConfigTypeSignature{
						ConfigType: api.ConfigType{
							ToolchainType: toolchain,
							ProviderType:  provider,
							LiveStateType: workerapi.ToolchainKubernetesYAML,
						},
					},
					AvailableTargets: targets,
				},
			},
		}
	}

	// Create targets for each context
	// Ensure targets are unique
	var targets []api.Target
	for contextName := range kubeConfig.Contexts {
		targets = append(targets, api.Target{
			BridgeHandle: contextName,
			Name:         api.GenerateTargetName(opts.WorkerSlug, provider, toolchain, contextName),
			Params: KubernetesWorkerParams{
				KubeContext: contextName,
				WaitTimeout: LargeWaitTimeout.String(),
			}.ToMap(),
		})
		// Add legacy target for backward compatibility
		if toolchain == workerapi.ToolchainKubernetesYAML {
			legacyTargetName := generateLegacyKubernetesTargetName(opts.WorkerSlug, contextName)
			targets = append(targets, api.Target{
				BridgeHandle: contextName,
				Name:         legacyTargetName,
				Params: KubernetesWorkerParams{
					KubeContext: contextName,
					WaitTimeout: LargeWaitTimeout.String(),
				}.ToMap(),
			})
		}
	}

	return api.BridgeWorkerInfo{
		SupportedConfigTypes: []*api.SupportedConfigType{
			{
				ConfigTypeSignature: api.ConfigTypeSignature{
					ConfigType: api.ConfigType{
						ToolchainType: toolchain,
						ProviderType:  provider,
						LiveStateType: workerapi.ToolchainKubernetesYAML,
					},
				},
				AvailableTargets: targets,
			},
		},
	}
}

func (w *KubernetesBridgeWorker) Apply(wctx api.BridgeWorkerContext, payload api.BridgeWorkerPayload) error {
	applier, err := w.GetOrCreateApplier(payload)
	if err != nil {
		return lib.SafeSendStatus(wctx, common.NewActionResult(
			api.ActionStatusFailed,
			api.ActionResultApplyFailed,
			err.Error(),
		), err)
	}

	objects, err := ParseObjects(payload.Data)
	if err != nil {
		return lib.SafeSendStatus(wctx, common.NewActionResult(
			api.ActionStatusFailed,
			api.ActionResultApplyFailed,
			err.Error(),
		), err)
	}

	if err := wctx.SendStatus(common.NewActionResult(
		api.ActionStatusProgressing,
		api.ActionResultNone,
		"Starting to apply resources...",
	)); err != nil {
		return err
	}

	log.Log.Info("🔄 Applying resources...", "count", len(objects))
	if err := wctx.SendStatus(common.NewActionResult(
		api.ActionStatusProgressing,
		api.ActionResultNone,
		"Applying resources...",
	)); err != nil {
		return err
	}

	result := applier.Apply(wctx.Context(), objects)
	if result.Error != nil {
		// Skip sending Failed status for interrupted operations - status already sent
		if errors.Is(result.Error, ErrOperationInterrupted) {
			return nil
		}
		return lib.SafeSendStatus(wctx, common.NewActionResult(
			api.ActionStatusFailed,
			api.ActionResultApplyFailed,
			result.Error.Error(),
		), result.Error)
	}

	// Log changeset entries if available (will be empty for non-blocking Apply)
	changesetCount := 0
	if result.ResourceSet != nil {
		changesetCount = len(result.ResourceSet.GetEntries())
	}
	log.Log.Info("✅ Successfully initiated applying resources", "changeset_entries", changesetCount)

	// Send ApplySynced to indicate config is pushed to target, but resources may not be ready yet
	// This enables the "Synced but not Ready" state distinction
	// The real LiveData will be sent after WaitForApply when we have live resources
	actionResult := common.NewActionResult(
		api.ActionStatusProgressing,
		api.ActionResultApplySynced,
		"Resources applied successfully, waiting for ready state",
	)
	// Don't set LiveData - let backend keep using its current state
	// Include per-resource status from Apply (initial status, mostly "InProgress")
	actionResult.ResourceStatuses = result.ResourceStatuses

	return wctx.SendStatus(actionResult)
}

func ObjectsToYAML(objects []*unstructured.Unstructured) (string, error) {
	yamlData, err := ssautil.ObjectsToYAML(objects)
	if err != nil {
		return "", err
	}
	// ObjectsToYAML adds a trailing doc separator. Remove it.
	return gaby.NormalizeYAML(yamlData), err
}

// createApplierConfig creates an ApplierConfig from the payload
func createApplierConfig(payload api.BridgeWorkerPayload) (ApplierConfig, error) {
	workerParams, kubeContext, err := ParseTargetParams(payload)
	if err != nil {
		return ApplierConfig{}, fmt.Errorf("failed to parse target params: %v", err)
	}

	return ApplierConfig{
		KubeContext:  kubeContext,
		LiveData:     payload.LiveData,
		SpaceID:      payload.SpaceID.String(),
		UnitSlug:     payload.UnitSlug,
		RevisionNum:  payload.RevisionNum,
		WaitTimeout:  workerParams.WaitTimeout,
	}, nil
}

// GetOrCreateApplier returns the applier instance, creating it if needed
func (w *KubernetesBridgeWorker) GetOrCreateApplier(payload api.BridgeWorkerPayload) (K8sApplier, error) {
	// Default to CLIUtilsSSA if applierType is not set (defensive programming)
	if w.applierType == "" {
		w.applierType = CLIUtilsSSA
		log.Log.Info("⚠️ Applier type was not set, defaulting to CLIUtilsSSA")
	}

	// For CLI Utils SSA, create a fresh applier each time
	if w.applierType == CLIUtilsSSA {
		applierConfig, err := createApplierConfig(payload)
		if err != nil {
			return nil, err
		}
		applier, err := NewK8sApplier(CLIUtilsSSA, applierConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create applier: %v", err)
		}
		log.Log.Info("📦 Created fresh applier for unit", "unitID", payload.UnitID.String())
		return applier, nil
	}

	// For other applier types, use cached instance
	if w.applier != nil {
		return w.applier, nil
	}

	applierConfig, err := createApplierConfig(payload)
	if err != nil {
		return nil, err
	}

	// Ensure we have a valid applier type (defensive)
	applierType := w.applierType
	if applierType == "" {
		applierType = CLIUtilsSSA
		log.Log.Info("⚠️ Applier type was empty, using CLIUtilsSSA as fallback")
	}
	applier, err := NewK8sApplier(applierType, applierConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create applier: %v", err)
	}

	w.applier = applier
	return w.applier, nil
}

func (w *KubernetesBridgeWorker) WatchForApply(wctx api.BridgeWorkerContext, payload api.BridgeWorkerPayload) error {
	log.Log.Info("🔄 Waiting for resources to be ready...")
	workerParams, _, err := ParseTargetParams(payload)
	if err != nil {
		// if we can't parse the target params, we cannot look for the resources
		return backoff.Permanent(lib.SafeSendStatus(wctx, common.NewActionResult(
			api.ActionStatusFailed,
			api.ActionResultApplyWaitFailed,
			err.Error(),
		), err))
	}

	applier, err := w.GetOrCreateApplier(payload)
	if err != nil {
		return lib.SafeSendStatus(wctx, common.NewActionResult(
			api.ActionStatusFailed,
			api.ActionResultApplyWaitFailed,
			err.Error(),
		), err)
	}

	objects, err := ParseObjects(payload.Data)
	if err != nil {
		// if we can't parse the objects, we can't wait for them
		return backoff.Permanent(lib.SafeSendStatus(wctx, common.NewActionResult(
			api.ActionStatusFailed,
			api.ActionResultApplyWaitFailed,
			err.Error(),
		), err))
	}

	if err := wctx.SendStatus(common.NewActionResult(
		api.ActionStatusProgressing,
		api.ActionResultNone,
		"Waiting for the applied resources...",
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

	// TODO: do we throw an error if the wait times out?
	// Default behavior is to wait 2m0s
	waitResult := applier.WaitForApply(wctx.Context(), objects, timeout)
	if waitResult.Error != nil {
		// Check if cancelled first
		if errors.Is(waitResult.Error, context.Canceled) {
			log.Log.Info("⚠️ Apply wait cancelled, not sending failure status")
			return nil
		}

		// Skip sending Failed status for interrupted operations - status already sent
		if errors.Is(waitResult.Error, ErrOperationInterrupted) {
			return nil
		}

		// Check context before sending failure
		select {
		case <-wctx.Context().Done():
			log.Log.Info("⚠️ Apply operation was cancelled/overridden, not sending failure status")
			return nil // Operation terminated, don't send failure
		default:
			// Continue to failure handling
		}

		log.Log.Error(waitResult.Error, "Failed to wait for resources")
		if errors.Is(waitResult.Error, context.DeadlineExceeded) {
			// Use exponential backoff for retry (retries forever, capped at 5min intervals)
			unitID := payload.UnitID.String()
			b := CreateRetryBackoff(workerParams)
			nextBackoff := b.NextBackOff()

			log.Log.Info("Resources not ready, retrying with exponential backoff",
				"unitID", unitID,
				"nextRetryIn", nextBackoff.String())

			lib.SafeSendStatus(wctx, common.NewActionResult(
				api.ActionStatusProgressing,
				api.ActionResultNone,
				fmt.Sprintf("Resources not ready, retrying in %v: %v", nextBackoff, waitResult.Error),
			), waitResult.Error)

			return backoff.RetryAfter(int(nextBackoff.Seconds()))
		}
		return lib.SafeSendStatus(wctx, common.NewActionResult(
			api.ActionStatusFailed,
			api.ActionResultApplyWaitFailed,
			waitResult.Error.Error(),
		), waitResult.Error)
	}

	// Success - no retry state to clear since we're stateless now

	// waitResult.LiveObjects contains UNCLEANED live objects from the cluster
	// We use uncleaned objects for LiveState (status tracking) and cleaned objects for LiveData (config storage)

	// Convert uncleaned objects to YAML for LiveState
	yamlDataForLiveState, err := ObjectsToYAML(waitResult.LiveObjects)
	if err != nil {
		log.Log.Error(err, "Failed to convert objects to YAML for LiveState")
		return lib.SafeSendStatus(wctx, common.NewActionResult(
			api.ActionStatusFailed,
			api.ActionResultApplyWaitFailed,
			fmt.Sprintf("Failed to convert objects to YAML: %v", err),
		), err)
	}

	// Cleanup objects for LiveData (remove managed fields, status, etc.)
	cleanedObjects := CleanupObjects(waitResult.LiveObjects)
	yamlDataForLiveData, err := ObjectsToYAML(cleanedObjects)
	if err != nil {
		log.Log.Error(err, "Failed to convert cleaned objects to YAML for LiveData")
		return lib.SafeSendStatus(wctx, common.NewActionResult(
			api.ActionStatusFailed,
			api.ActionResultApplyWaitFailed,
			fmt.Sprintf("Failed to convert cleaned objects to YAML: %v", err),
		), err)
	}

	// Handle inventory in LiveData for CLI Utils applier
	liveDataData := []byte(yamlDataForLiveData)
	if w.applierType == CLIUtilsSSA {
		// Update LiveData with inventory for CLI Utils SSA applier. This should not destroy yamlDataForLiveData.
		liveDataData, err = w.UpdateLiveDataWithInventory(wctx.Context(), payload, cleanedObjects, []byte(yamlDataForLiveData))
		if err != nil {
			log.Log.Error(err, "⚠️ Failed to update LiveData with inventory, using raw YAML")
			liveDataData = []byte(yamlDataForLiveData)
		}
	}

	// Check if operation was cancelled/overridden while waiting
	select {
	case <-wctx.Context().Done():
		log.Log.Info("⚠️ Apply operation was cancelled/overridden, skipping completion status")
		return nil
	default:
		// Continue to send completed status
	}

	status := common.NewActionResult(
		api.ActionStatusCompleted,
		api.ActionResultApplyCompleted,
		fmt.Sprintf("Applied %d resources successfully at %s", len(waitResult.LiveObjects), time.Now().Format(time.RFC3339)),
	)
	// LiveData contains cleaned objects (for config storage/drift detection)
	// LiveState contains uncleaned objects (for status tracking with full metadata)
	status.LiveData = liveDataData
	status.LiveState = []byte(yamlDataForLiveState)
	// Include per-resource sync and readiness status
	status.ResourceStatuses = waitResult.ResourceStatuses

	wctx.SendStatus(status)
	return nil
}

// UpdateLiveDataWithInventory updates the LiveData to include inventory ConfigMap as the first document
func (w *KubernetesBridgeWorker) UpdateLiveDataWithInventory(ctx context.Context, payload api.BridgeWorkerPayload, liveObjects []*unstructured.Unstructured, yamlResources []byte) ([]byte, error) {
	// Create inventory info
	invInfo := &SimpleInventoryInfo{
		namespace: "default",
		name:      "confighub-inventory",
		id:        fmt.Sprintf("%s-%s", payload.SpaceID.String(), payload.UnitSlug),
	}

	// Create a new inventory client for this operation
	invClient := NewInMemInventoryClient()

	// Extract existing inventory ConfigMap from LiveData if present
	var inventoryCM *InventoryConfigMap
	if len(payload.LiveData) > 0 {
		_, existingCM, _, err := CreateInventoryFromLiveData(ctx, payload.LiveData, invInfo)
		if err != nil {
			log.Log.Error(err, "Failed to extract inventory from LiveData")
			// Create new inventory ConfigMap
			inventoryCM = NewInventoryConfigMap(invInfo)
		} else {
			inventoryCM = existingCM
		}
	} else {
		inventoryCM = NewInventoryConfigMap(invInfo)
	}

	// Update inventory with current objects
	var objRefs object.ObjMetadataSet
	for _, obj := range liveObjects {
		objRefs = append(objRefs, object.ObjMetadata{
			GroupKind: schema.GroupKind{
				Group: obj.GroupVersionKind().Group,
				Kind:  obj.GetKind(),
			},
			Namespace: obj.GetNamespace(),
			Name:      obj.GetName(),
		})
	}

	// Update inventory client
	inv, err := invClient.NewInventory(invInfo)
	if err != nil {
		return nil, fmt.Errorf("failed to create new inventory: %w", err)
	}
	inv.SetObjectRefs(objRefs)
	err = invClient.CreateOrUpdate(ctx, inv, inventory.UpdateOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to update inventory: %w", err)
	}

	// Save inventory to LiveData
	liveData, err := SaveInventoryToLiveData(invClient, inventoryCM, invInfo, yamlResources)
	if err != nil {
		return nil, fmt.Errorf("failed to save inventory to LiveData: %w", err)
	}

	log.Log.Info("📦 Updated LiveData with inventory", "inventoryID", invInfo.GetID(), "objects", len(objRefs))
	return liveData, nil
}

func (w *KubernetesBridgeWorker) Refresh(wctx api.BridgeWorkerContext, payload api.BridgeWorkerPayload) error {
	var refreshParams *api.RefreshParams
	if len(payload.ExtraParams) > 0 {
		// Try to parse ExtraParams as RefreshParams structure
		refreshParams = new(api.RefreshParams)
		if err := json.Unmarshal(payload.ExtraParams, refreshParams); err != nil {
			refreshParams = nil
		}
	}

	applier, err := w.GetOrCreateApplier(payload)
	if err != nil {
		return lib.SafeSendStatus(wctx, common.NewActionResult(
			api.ActionStatusFailed,
			api.ActionResultRefreshFailed,
			err.Error(),
		), err)
	}

	objects, err := ParseObjects(payload.Data)
	if err != nil {
		return lib.SafeSendStatus(wctx, common.NewActionResult(
			api.ActionStatusFailed,
			api.ActionResultRefreshFailed,
			err.Error(),
		), err)
	}

	if err := wctx.SendStatus(common.NewActionResult(
		api.ActionStatusProgressing,
		api.ActionResultNone,
		"Starting to retrieve resources...",
	)); err != nil {
		return err
	}

	log.Log.Info("🔄 Retrieving resources...", "count", len(objects))
	if err := wctx.SendStatus(common.NewActionResult(
		api.ActionStatusProgressing,
		api.ActionResultNone,
		"Retrieving resources...",
	)); err != nil {
		return err
	}

	// retrievedObjects contains UNCLEANED live objects from the cluster
	retrievedObjects, err := applier.Refresh(wctx.Context(), objects)
	if err != nil {
		return lib.SafeSendStatus(wctx, common.NewActionResult(
			api.ActionStatusFailed,
			api.ActionResultRefreshFailed,
			err.Error(),
		), err)
	}

	// Convert uncleaned objects to YAML for LiveState (status tracking with full metadata)
	yamlDataForLiveState, err := ObjectsToYAML(retrievedObjects)
	if err != nil {
		log.Log.Error(err, "Failed to convert objects to YAML for LiveState")
		return lib.SafeSendStatus(wctx, common.NewActionResult(
			api.ActionStatusFailed,
			api.ActionResultRefreshFailed,
			fmt.Sprintf("Failed to convert objects to YAML for LiveState: %v", err),
		), err)
	}

	// Apply extra cleanup (removes status, managed fields, internal annotations, etc.) for LiveData
	// Note: extraCleanupObjects modifies objects in-place, so we do this after converting to YAML for LiveState
	yamlData, err := ObjectsToYAML(ExtraCleanupObjects(retrievedObjects))
	if err != nil {
		log.Log.Error(err, "Failed to convert cleaned objects to YAML for LiveData")
		return lib.SafeSendStatus(wctx, common.NewActionResult(
			api.ActionStatusFailed,
			api.ActionResultRefreshFailed,
			fmt.Sprintf("Failed to convert objects to YAML: %v", err),
		), err)
	}

	// Extract inventory from LiveData if present and preserve it
	inventoryCM, _, err := SplitInventoryFromLiveData(payload.LiveData)
	if err != nil {
		log.Log.Error(err, "Failed to split inventory from LiveData, continuing without inventory")
	}

	// TODO: Known issue - namespace handling is not optimal here.
	// When we split inventory from LiveData, we may lose namespace context
	// for resources that don't explicitly specify a namespace in their YAML.
	// This could lead to resources being applied to the wrong namespace.
	// Future improvement: Ensure namespace is properly preserved during split.

	// Perform diff patch on resources only (without inventory) to detect drift
	//
	// We use the base Data provided by the server, if provided.
	//
	// Note that we don't follow the delegation principle used by kubectl apply here.
	// If we did, we'd only report drift on previously applied fields. We also add fields
	// from the live state, because a lot of configuration changes entail adding optional
	// fields. We'll need to add an ignore mechanism at some point (e.g., if/when we make
	// refresh continuous) to ignore dynamically changed fields, such as autoscaled replicas.
	//
	// Note: We strip comments from baseData before calling DiffPatchWithOptions because
	// the yamlData retrieved from the Kubernetes cluster does not contain comments.
	// This ensures we don't treat comment differences as drift, and allows comments to be
	// preserved in the final patched result.
	var baseData []byte
	if refreshParams == nil {
		baseData = payload.Data
	} else {
		baseData = refreshParams.BaseRevisionData
	}

	// Strip comments from baseData to avoid treating comment differences as drift
	baseDataWithoutComments, err := yamlkit.StripComments(baseData)
	if err != nil {
		log.Log.Error(err, "Failed to strip comments from baseData, continuing without stripping")
		baseDataWithoutComments = baseData
	}

	patched, drifted, err := yamlkit.DiffPatchWithOptions(baseDataWithoutComments, []byte(yamlData), payload.Data, w.resourceProvider, false)
	if err != nil {
		log.Log.Error(err, "Failed to diff patch")
		return lib.SafeSendStatus(wctx, common.NewActionResult(
			api.ActionStatusFailed,
			api.ActionResultRefreshFailed,
			fmt.Sprintf("Failed to diff patch: %v", err),
		), err)
	}

	if !drifted {
		log.Log.Info("✅ No drift detected")
		result := common.NewActionResult(
			api.ActionStatusCompleted,
			api.ActionResultRefreshAndNoDrift,
			"Live state matches - no drift detected",
		)
		// Even when there's no drift, update LiveData with refreshed resources but preserve inventory
		if inventoryCM != nil {
			updatedLiveData, err := CombineInventoryWithResources(inventoryCM, []byte(yamlData))
			if err != nil {
				log.Log.Error(err, "Failed to combine inventory with refreshed resources")
				result.LiveData = []byte(yamlData)
			} else {
				result.LiveData = updatedLiveData
			}
		} else {
			result.LiveData = []byte(yamlData)
		}
		// LiveState contains uncleaned objects (with full metadata for status tracking)
		result.LiveState = []byte(yamlDataForLiveState)
		return wctx.SendStatus(result)
	}

	log.Log.Info("✅ Successfully retrieved resources", "count", len(retrievedObjects))

	// Combine inventory with refreshed resources for the final LiveData
	var updatedLiveData []byte
	if inventoryCM != nil {
		updatedLiveData, err = CombineInventoryWithResources(inventoryCM, []byte(yamlData))
		if err != nil {
			log.Log.Error(err, "Failed to combine inventory with refreshed resources")
			updatedLiveData = []byte(yamlData)
		}
	} else {
		updatedLiveData = []byte(yamlData)
	}

	result := common.NewActionResult(
		api.ActionStatusCompleted,
		api.ActionResultRefreshAndDrifted,
		fmt.Sprintf("Retrieved %d resources successfully at %s", len(retrievedObjects), time.Now().Format(time.RFC3339)),
	)
	result.Data = patched
	result.LiveData = updatedLiveData // Use LiveData with preserved inventory
	// LiveState contains uncleaned objects (with full metadata for status tracking)
	result.LiveState = []byte(yamlDataForLiveState)
	return wctx.SendStatus(result)
}

func (w *KubernetesBridgeWorker) Import(wctx api.BridgeWorkerContext, payload api.BridgeWorkerPayload) (retErr error) {
	// Add panic recovery with stack trace when debugging
	// defer func() {
	// 	if r := recover(); r != nil {
	// 		retErr = errors.WithStack(errors.Newf("panic in Kubernetes Import: %v", r))
	// 		log.Log.Error(retErr, "Panic recovered in Kubernetes Import",
	// 			"unitSlug", payload.UnitSlug,
	// 			"panic", r)
	// 	}
	// }()

	_, kubeContext, err := ParseTargetParams(payload)
	if err != nil {
		return lib.SafeSendStatus(wctx, common.NewActionResult(
			api.ActionStatusFailed,
			api.ActionResultImportFailed,
			err.Error(),
		), err)
	}

	k8sclient, man, err := KubernetesClientFactory(kubeContext)
	if err != nil {
		return lib.SafeSendStatus(wctx, common.NewActionResult(
			api.ActionStatusFailed,
			api.ActionResultImportFailed,
			err.Error(),
		), err)
	}

	var retrievedObjects []*unstructured.Unstructured

	// Determine import source and get resource list
	var importRequest *goclientnew.ImportRequest
	if len(payload.ExtraParams) > 0 {
		// Try to parse ExtraParams as ImportRequest structure
		importRequest = new(goclientnew.ImportRequest)
		if err := json.Unmarshal(payload.ExtraParams, importRequest); err != nil {
			importRequest = nil
		}
	}

	if importRequest != nil && importRequest.ResourceInfoList != nil && len(*importRequest.ResourceInfoList) > 0 {
		resourceInfoList := *importRequest.ResourceInfoList

		// Legacy flow: ResourceInfoList is provided via stdin/file.
		if err := wctx.SendStatus(common.NewActionResult(
			api.ActionStatusProgressing,
			api.ActionResultNone,
			fmt.Sprintf("Found %d resources to import", len(resourceInfoList)),
		)); err != nil {
			return err
		}

		// Convert ResourceInfoList to Unstructured objects
		objects := []*unstructured.Unstructured{}
		for _, resourceInfo := range resourceInfoList {
			u := &unstructured.Unstructured{}
			gvk, err := ParseGroupVersionKind(resourceInfo.ResourceType)
			if err != nil {
				return lib.SafeSendStatus(wctx, common.NewActionResult(
					api.ActionStatusFailed,
					api.ActionResultImportFailed,
					err.Error(),
				), err)
			}
			u.SetGroupVersionKind(gvk)
			parts := strings.Split(resourceInfo.ResourceName, "/")
			if len(parts) == 2 {
				u.SetNamespace(parts[0])
				u.SetName(parts[1])
			} else if len(parts) == 1 {
				u.SetName(resourceInfo.ResourceName)
			}
			objects = append(objects, u)
		}

		// Only get live objects if we're importing from stdin/file (legacy flow)
		if err := wctx.SendStatus(common.NewActionResult(
			api.ActionStatusProgressing,
			api.ActionResultNone,
			"Retrieving live state of resources...",
		)); err != nil {
			return err
		}

		// Return uncleaned objects - we'll cleanup for LiveData below, keep uncleaned for LiveState
		retrievedObjects, err = GetLiveObjects(wctx.Context(), man.Client(), objects, false, false)
		if err != nil {
			log.Log.Error(err, "Failed to retrieve live objects")
			return lib.SafeSendStatus(wctx, common.NewActionResult(
				api.ActionStatusFailed,
				api.ActionResultImportFailed,
				fmt.Sprintf("Failed to retrieve live objects: %v", err),
			), err)
		}
	} else {
		// New flow: Fetch resources from cluster using parameters
		if err := wctx.SendStatus(common.NewActionResult(
			api.ActionStatusProgressing,
			api.ActionResultNone,
			"Fetching resources from Kubernetes cluster...",
		)); err != nil {
			return err
		}

		retrievedObjects, err = w.getResourcesFromImportSource(k8sclient, importRequest)
		if err != nil {
			return lib.SafeSendStatus(wctx, common.NewActionResult(
				api.ActionStatusFailed,
				api.ActionResultImportFailed,
				fmt.Sprintf("Failed to get cluster resources: %v", err),
			), err)
		}
	}

	if err := wctx.SendStatus(common.NewActionResult(
		api.ActionStatusProgressing,
		api.ActionResultNone,
		"Converting resources to YAML format...",
	)); err != nil {
		return err
	}

	// Convert uncleaned objects to YAML for LiveState (status tracking with full metadata)
	// Must be done BEFORE extraCleanupObjects which modifies objects in place
	yamlForLiveState, err := ObjectsToYAML(retrievedObjects)
	if err != nil {
		log.Log.Error(err, "Failed to convert objects to YAML for LiveState")
		return lib.SafeSendStatus(wctx, common.NewActionResult(
			api.ActionStatusFailed,
			api.ActionResultImportFailed,
			fmt.Sprintf("Failed to convert live state objects to YAML: %v", err),
		), err)
	}

	// Apply extra cleanup (removes status, managed fields, internal annotations, etc.)
	// This makes objects suitable for being unit.Data
	// Note: extraCleanupObjects modifies objects in-place
	cleanedObjects := ExtraCleanupObjects(retrievedObjects)
	yamlForData, err := ObjectsToYAML(cleanedObjects)
	if err != nil {
		log.Log.Error(err, "Failed to convert objects to YAML for Data")
		return lib.SafeSendStatus(wctx, common.NewActionResult(
			api.ActionStatusFailed,
			api.ActionResultImportFailed,
			fmt.Sprintf("Failed to convert data objects to YAML: %v", err),
		), err)
	}

	// Use cleaned objects for LiveData (config storage/inventory)
	yamlForLiveData := yamlForData

	// Create inventory for imported resources

	// Create inventory info
	// Use default values for SpaceID and UnitSlug since they're not in KubernetesWorkerParams
	invInfo := SimpleInventoryInfo{
		id:        DefaultInventoryID,
		name:      DefaultInventoryName,
		namespace: DefaultNamespace,
	}

	// Create a new inventory client for this operation
	invClient := NewInMemInventoryClient()
	inventoryCM := NewInventoryConfigMap(&invInfo)

	// Add imported objects to inventory
	var objRefs object.ObjMetadataSet
	for _, obj := range retrievedObjects {
		objRefs = append(objRefs, object.ObjMetadata{
			GroupKind: schema.GroupKind{
				Group: obj.GroupVersionKind().Group,
				Kind:  obj.GetKind(),
			},
			// For cluster-scoped resources (like ClusterRole, PersistentVolume),
			// GetNamespace() returns an empty string which is correct.
			// The inventory client handles this properly by not setting namespace
			// metadata for cluster-scoped resources.
			Namespace: obj.GetNamespace(),
			Name:      obj.GetName(),
		})
	}

	// Replace inventory with imported objects
	inv, err := invClient.NewInventory(&invInfo)
	if err != nil {
		log.Log.Error(err, "Failed to create new inventory")
		// Continue without inventory tracking
		result := common.NewActionResult(
			api.ActionStatusCompleted,
			api.ActionResultImportCompleted,
			fmt.Sprintf("Imported %d resources successfully at %s (inventory tracking failed but resources imported)", len(retrievedObjects), time.Now().Format(time.RFC3339)),
		)
		result.Data = []byte(yamlForData)
		result.LiveData = []byte(yamlForLiveData)
		// LiveState contains uncleaned objects (with full metadata for status tracking)
		result.LiveState = []byte(yamlForLiveState)
		return wctx.SendStatus(result)
	}

	inv.SetObjectRefs(objRefs)
	err = invClient.CreateOrUpdate(wctx.Context(), inv, inventory.UpdateOptions{})
	if err != nil {
		log.Log.Error(err, "Failed to update inventory with imported objects")
		// Continue without inventory tracking
		result := common.NewActionResult(
			api.ActionStatusCompleted,
			api.ActionResultImportCompleted,
			fmt.Sprintf("Imported %d resources successfully at %s (inventory update failed but resources imported)", len(retrievedObjects), time.Now().Format(time.RFC3339)),
		)
		result.Data = []byte(yamlForData)
		result.LiveData = []byte(yamlForLiveData)
		// LiveState contains uncleaned objects (with full metadata for status tracking)
		result.LiveState = []byte(yamlForLiveState)
		return wctx.SendStatus(result)
	}

	// Save inventory to LiveData
	liveDataWithInventory, err := SaveInventoryToLiveData(invClient, inventoryCM, &invInfo, []byte(yamlForLiveData))
	if err != nil {
		log.Log.Error(err, "Failed to save inventory to LiveData")
		// Continue without inventory in LiveData
		result := common.NewActionResult(
			api.ActionStatusCompleted,
			api.ActionResultImportCompleted,
			fmt.Sprintf("Imported %d resources successfully at %s (inventory not saved to LiveData but resources imported)", len(retrievedObjects), time.Now().Format(time.RFC3339)),
		)
		result.Data = []byte(yamlForData)
		result.LiveData = []byte(yamlForLiveData)
		// LiveState contains uncleaned objects (with full metadata for status tracking)
		result.LiveState = []byte(yamlForLiveState)
		return wctx.SendStatus(result)
	}

	log.Log.Info("📦 Created inventory for imported resources", "inventoryID", invInfo.id, "objects", len(objRefs))

	result := common.NewActionResult(
		api.ActionStatusCompleted,
		api.ActionResultImportCompleted,
		fmt.Sprintf("Imported %d resources successfully at %s", len(retrievedObjects), time.Now().Format(time.RFC3339)),
	)
	result.Data = []byte(yamlForData)
	result.LiveData = liveDataWithInventory
	// LiveState contains uncleaned objects (with full metadata for status tracking)
	result.LiveState = []byte(yamlForLiveState)
	return wctx.SendStatus(result)
}

func (w *KubernetesBridgeWorker) Destroy(wctx api.BridgeWorkerContext, payload api.BridgeWorkerPayload) error {
	applier, err := w.GetOrCreateApplier(payload)
	if err != nil {
		return lib.SafeSendStatus(wctx, common.NewActionResult(
			api.ActionStatusFailed,
			api.ActionResultDestroyFailed,
			err.Error(),
		), err)
	}

	objects, err := ParseObjects(payload.Data)
	if err != nil {
		return lib.SafeSendStatus(wctx, common.NewActionResult(
			api.ActionStatusFailed,
			api.ActionResultDestroyFailed,
			err.Error(),
		), err)
	}

	if err = wctx.SendStatus(common.NewActionResult(
		api.ActionStatusProgressing,
		api.ActionResultNone,
		"Starting to destroy resources...",
	)); err != nil {
		return err
	}

	result := applier.Destroy(wctx.Context(), objects)
	if result.Error != nil {
		// Skip sending Failed status for interrupted operations - status already sent
		if errors.Is(result.Error, ErrOperationInterrupted) {
			return nil
		}
		return lib.SafeSendStatus(wctx, common.NewActionResult(
			api.ActionStatusFailed,
			api.ActionResultDestroyFailed,
			result.Error.Error(),
		), result.Error)
	}

	// Log changeset entries if available (will be empty for non-blocking Destroy)
	changesetCount := 0
	if result.ResourceSet != nil {
		changesetCount = len(result.ResourceSet.GetEntries())
	}
	log.Log.Info("✅ Successfully initiated destruction of resources", "changeset_entries", changesetCount)
	return nil
}

func (w *KubernetesBridgeWorker) WatchForDestroy(wctx api.BridgeWorkerContext, payload api.BridgeWorkerPayload) error {
	log.Log.Info("🔄 Waiting for resources to be terminated...")
	workerParams, _, err := ParseTargetParams(payload)
	if err != nil {
		return lib.SafeSendStatus(wctx, common.NewActionResult(
			api.ActionStatusFailed,
			api.ActionResultDestroyWaitFailed,
			err.Error(),
		), err)
	}

	applier, err := w.GetOrCreateApplier(payload)
	if err != nil {
		return lib.SafeSendStatus(wctx, common.NewActionResult(
			api.ActionStatusFailed,
			api.ActionResultDestroyWaitFailed,
			err.Error(),
		), err)
	}

	objects, err := ParseObjects(payload.Data)
	if err != nil {
		return lib.SafeSendStatus(wctx, common.NewActionResult(
			api.ActionStatusFailed,
			api.ActionResultDestroyWaitFailed,
			err.Error(),
		), err)
	}

	if err = wctx.SendStatus(common.NewActionResult(
		api.ActionStatusProgressing,
		api.ActionResultNone,
		"Waiting for resources to be terminated...",
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

	waitResult := applier.WaitForDestroy(wctx.Context(), objects, timeout)
	if waitResult.Error != nil {
		// Check if cancelled first
		if errors.Is(waitResult.Error, context.Canceled) {
			log.Log.Info("⚠️ Destroy wait cancelled, not sending failure status")
			return nil
		}

		// Skip sending Failed status for interrupted operations - status already sent
		if errors.Is(waitResult.Error, ErrOperationInterrupted) {
			return nil
		}

		// Check context before sending failure
		select {
		case <-wctx.Context().Done():
			log.Log.Info("⚠️ Destroy operation was cancelled/overridden, not sending failure status")
			return nil // Operation terminated, don't send failure
		default:
			// Continue to failure handling
		}

		log.Log.Error(waitResult.Error, "Failed to wait for resource termination")
		if errors.Is(waitResult.Error, context.DeadlineExceeded) {
			// Use exponential backoff for retry (retries forever, capped at 5min intervals)
			unitID := payload.UnitID.String()
			b := CreateRetryBackoff(workerParams)
			nextBackoff := b.NextBackOff()

			log.Log.Info("Resources not terminated yet, retrying with exponential backoff",
				"unitID", unitID,
				"nextRetryIn", nextBackoff.String())

			lib.SafeSendStatus(wctx, common.NewActionResult(
				api.ActionStatusProgressing,
				api.ActionResultNone,
				fmt.Sprintf("Resources not terminated, retrying in %v: %v", nextBackoff, waitResult.Error),
			), waitResult.Error)

			return backoff.RetryAfter(int(nextBackoff.Seconds()))
		}
		return lib.SafeSendStatus(wctx, common.NewActionResult(
			api.ActionStatusFailed,
			api.ActionResultDestroyWaitFailed,
			waitResult.Error.Error(),
		), waitResult.Error)
	}

	// Success - no retry state to clear since we're stateless now

	// waitResult.LiveObjects contains UNCLEANED remaining objects (if any) from the cluster
	// After successful destroy, this is typically empty

	// Convert uncleaned objects to YAML for LiveState (status tracking with full metadata)
	var yamlDataForLiveState string
	if len(waitResult.LiveObjects) > 0 {
		var err error
		yamlDataForLiveState, err = ObjectsToYAML(waitResult.LiveObjects)
		if err != nil {
			log.Log.Error(err, "Failed to convert remaining objects to YAML for LiveState")
			yamlDataForLiveState = ""
		}
	}

	// For CLIUtils applier, use the LiveData from LiveDataBuilder
	// For other appliers, build LiveData from the returned LiveObjects
	var liveDataData []byte
	if w.applierType == CLIUtilsSSA {
		// CLIUtils already built the LiveData with LiveDataBuilder
		// Convert cleaned LiveObjects to YAML and add inventory
		if len(waitResult.LiveObjects) > 0 {
			// Cleanup objects for LiveData
			cleanedObjects := CleanupObjects(waitResult.LiveObjects)
			yamlData, err := ObjectsToYAML(cleanedObjects)
			if err != nil {
				log.Log.Error(err, "Failed to convert remaining objects to YAML")
				liveDataData = []byte{}
			} else {
				// Add inventory for remaining resources
				liveDataData, err = w.UpdateLiveDataWithInventory(wctx.Context(), payload, cleanedObjects, []byte(yamlData))
				if err != nil {
					log.Log.Error(err, "Failed to update LiveData with inventory")
					liveDataData = []byte(yamlData)
				}
			}
		} else {
			// All resources destroyed - return empty LiveData
			liveDataData = []byte{}
		}
	} else {
		// For other appliers, check if anything remains
		if len(waitResult.LiveObjects) > 0 {
			// Cleanup objects for LiveData
			cleanedObjects := CleanupObjects(waitResult.LiveObjects)
			yamlData, err := ObjectsToYAML(cleanedObjects)
			if err != nil {
				log.Log.Error(err, "Failed to convert remaining objects to YAML")
				liveDataData = []byte{}
			} else {
				liveDataData = []byte(yamlData)
			}
		} else {
			liveDataData = []byte{}
		}
	}

	// Log the ResourceSet if available
	changesetCount := 0
	if waitResult.ResourceSet != nil {
		changesetCount = len(waitResult.ResourceSet.GetEntries())
	}
	log.Log.Info("📦 Destroy completed",
		"deletedResources", changesetCount,
		"remainingResources", len(waitResult.LiveObjects))

	// Check if operation was cancelled/overridden while waiting
	select {
	case <-wctx.Context().Done():
		log.Log.Info("⚠️ Destroy operation was cancelled/overridden, skipping completion status")
		return nil
	default:
		// Continue to send completed status
	}

	result := common.NewActionResult(
		api.ActionStatusCompleted,
		api.ActionResultDestroyCompleted,
		fmt.Sprintf("Destroyed resources successfully at %s", time.Now().Format(time.RFC3339)),
	)
	// LiveData contains cleaned objects (for config storage)
	result.LiveData = liveDataData
	// LiveState contains uncleaned objects (with full metadata for status tracking)
	result.LiveState = []byte(yamlDataForLiveState)

	return wctx.SendStatus(result)
}

// BuildLiveData returns the live data YAML, optionally updated with inventory for CLI Utils applier.
func (w *KubernetesBridgeWorker) BuildLiveData(ctx context.Context, payload api.BridgeWorkerPayload, cleanedObjects []*unstructured.Unstructured, rawYAML string) []byte {
	data := []byte(rawYAML)
	if w.applierType == CLIUtilsSSA {
		liveDataWithInv, invErr := w.UpdateLiveDataWithInventory(ctx, payload, cleanedObjects, data)
		if invErr != nil {
			log.Log.Error(invErr, "Failed to update LiveData with inventory, using raw YAML")
		} else {
			data = liveDataWithInv
		}
	}
	return data
}

// Finalize implements api.BridgeWorker.Finalize
// This method is called when the worker is being shutdown or cleaned up
func (w *KubernetesBridgeWorker) Finalize(wctx api.BridgeWorkerContext, payload api.BridgeWorkerPayload) error {
	// No cleanup needed for stateless applier
	log.Log.Info("Finalizing Kubernetes bridge worker")
	return nil
}
