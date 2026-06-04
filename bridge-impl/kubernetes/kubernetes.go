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
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/cli-utils/pkg/object"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/confighub/sdk/bridge-impl/common"
	"github.com/confighub/sdk/bridge-impl/kubernetes/cleanup"
	"github.com/confighub/sdk/configkit/k8skit"
	"github.com/confighub/sdk/core/configkit/cubkit"
	"github.com/confighub/sdk/core/configkit/yamlkit"
	funcapi "github.com/confighub/sdk/core/function/api"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/confighub/sdk/core/third_party/gaby"
	"github.com/confighub/sdk/core/worker/api"
	"github.com/confighub/sdk/core/worker/lib"
	"github.com/confighub/sdk/core/workerapi"
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
	return &KubernetesBridgeWorker{
		applierType:      CLIUtilsSSA,
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

func GetDisableTargetCreation() bool {
	disableTargetVar := os.Getenv("CONFIGHUB_DISABLE_AUTO_TARGET_CREATION")
	return disableTargetVar != "" && disableTargetVar != "0" && strings.ToLower(disableTargetVar) != "false"
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

	// Optionally disable target create
	disableTargets := GetDisableTargetCreation()

	// All other bridges using the Kubernetes bridge internally are compatible and use the same BridgeHandle values
	compatibleBridge := api.ProviderKubernetes
	if provider == api.ProviderKubernetes {
		compatibleBridge = ""
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
		targetName := "" // no name means don't create a target

		if !disableTargets {
			targetName = os.Getenv("CONFIGHUB_IN_CLUSTER_TARGET_NAME")
			if targetName == "" {
				// TODO: Deprecated. Remove this eventually.
				targetName = os.Getenv("IN_CLUSTER_TARGET_NAME")
			}

			// Ensure target is unique
			if targetName == "" || toolchain != workerapi.ToolchainKubernetesYAML {
				defaultName = true
				targetName = api.GenerateTargetName(opts.WorkerSlug, provider, toolchain, "cluster")
			}
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
		if !disableTargets && defaultName && provider == api.ProviderKubernetes {
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
						Options: kubernetesBridgeOptions(),
					},
					CompatibleBridge: compatibleBridge,
					AvailableTargets: targets,
				},
			},
		}
	}

	// Create targets for each context
	// Ensure targets are unique
	var targets []api.Target
	for contextName := range kubeConfig.Contexts {
		targetName := "" // no name means don't create a target
		if !disableTargets {
			targetName = api.GenerateTargetName(opts.WorkerSlug, provider, toolchain, contextName)
		}
		targets = append(targets, api.Target{
			BridgeHandle: contextName,
			Name:         targetName,
			Params: KubernetesWorkerParams{
				KubeContext: contextName,
				WaitTimeout: LargeWaitTimeout.String(),
			}.ToMap(),
		})
		// Add legacy target for backward compatibility
		if !disableTargets && provider == api.ProviderKubernetes {
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
					Options: kubernetesBridgeOptions(),
				},
				CompatibleBridge: compatibleBridge,
				AvailableTargets: targets,
			},
		},
	}
}

// kubernetesBridgeOptions returns the BridgeOptions advertised by the Kubernetes bridge.
func kubernetesBridgeOptions() []api.BridgeOption {
	return []api.BridgeOption{
		{
			Name:        "Namespace",
			Description: "Default namespace for resources without an explicit metadata.namespace. If not set, falls back to the kubeconfig context's namespace, then \"default\".",
			Required:    false,
			DataType:    funcapi.DataTypeString,
			Example:     "production",
		},
		{
			Name:        "ProgressingTimeout",
			Description: "Go duration after which a resource still kstatus=InProgress with no controller signal is flagged Stuck (e.g. \"150s\", \"5m\"). Kind-specific Stuck classifiers always take precedence; this is the last-resort time-based fallback. Defaults to 150s.",
			Required:    false,
			DataType:    funcapi.DataTypeString,
			Example:     "150s",
		},
	}
}

// parseProgressingTimeout reads the ProgressingTimeout BridgeOption from the
// payload, returning zero if unset or unparseable. The statuspoller applies its
// own default (150s) when the value is zero.
func parseProgressingTimeout(payload api.BridgeWorkerPayload) time.Duration {
	raw, ok := payload.TargetOptions["ProgressingTimeout"]
	if !ok || raw == "" {
		return 0
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		log.Log.Info("⚠️ Invalid ProgressingTimeout, using default", "value", raw, "error", err.Error())
		return 0
	}
	return d
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

	result := applier.Apply(wctx, objects)
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

// CreateApplierConfig creates an ApplierConfig from the payload. Exported so
// renderer bridges can build an applier config and apply auxiliary resources
// (e.g. the Flux CR that the Flux renderer SSA-applies with spec.suspend=true).
func CreateApplierConfig(payload api.BridgeWorkerPayload) (ApplierConfig, error) {
	workerParams, kubeContext, err := ParseTargetParams(payload)
	if err != nil {
		return ApplierConfig{}, fmt.Errorf("failed to parse target params: %v", err)
	}

	return ApplierConfig{
		KubeContext:        kubeContext,
		EnforcedNamespace:  ResolveNamespace(payload),
		LiveData:           payload.LiveData,
		BridgeState:        payload.BridgeState,
		SpaceID:            payload.SpaceID.String(),
		UnitSlug:           payload.UnitSlug,
		RevisionNum:        payload.RevisionNum,
		WaitTimeout:        workerParams.WaitTimeout,
		DryRun:             payload.DryRun,
		ProgressingTimeout: parseProgressingTimeout(payload),
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
		applierConfig, err := CreateApplierConfig(payload)
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

	applierConfig, err := CreateApplierConfig(payload)
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
	// In dry run mode, resources were not actually applied, so skip waiting.
	if payload.DryRun {
		log.Log.Info("🔍 Dry run mode - skipping apply watch, resources were not mutated")
		return wctx.SendStatus(common.NewActionResult(
			api.ActionStatusCompleted,
			api.ActionResultApplyCompleted,
			"Dry run completed - resources validated but not applied",
		))
	}

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
	waitResult := applier.WaitForApply(wctx, objects, timeout)
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
	cleanedObjects := cleanup.CleanupObjects(waitResult.LiveObjects)
	yamlDataForLiveData, err := ObjectsToYAML(cleanedObjects)
	if err != nil {
		log.Log.Error(err, "Failed to convert cleaned objects to YAML for LiveData")
		return lib.SafeSendStatus(wctx, common.NewActionResult(
			api.ActionStatusFailed,
			api.ActionResultApplyWaitFailed,
			fmt.Sprintf("Failed to convert cleaned objects to YAML: %v", err),
		), err)
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
	// LiveData = cleaned resources (no inventory ConfigMap)
	// LiveState = uncleaned resources with status
	// BridgeState = inventory ConfigMap
	status.LiveData = []byte(yamlDataForLiveData)
	status.LiveState = []byte(yamlDataForLiveState)
	status.BridgeState = w.BuildBridgeState(payload, waitResult.LiveObjects)
	status.ResourceStatuses = waitResult.ResourceStatuses

	wctx.SendStatus(status)
	return nil
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
	retrievedObjects, err := applier.Refresh(wctx, objects)
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
	yamlData, err := ObjectsToYAML(cleanup.ExtraCleanupObjects(retrievedObjects))
	if err != nil {
		log.Log.Error(err, "Failed to convert cleaned objects to YAML for LiveData")
		return lib.SafeSendStatus(wctx, common.NewActionResult(
			api.ActionStatusFailed,
			api.ActionResultRefreshFailed,
			fmt.Sprintf("Failed to convert objects to YAML: %v", err),
		), err)
	}

	// Perform diff patch on resources only to detect drift
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

	patched, drifted, err := yamlkit.DiffPatchWithOptions(baseDataWithoutComments, []byte(yamlData), payload.Data, w.resourceProvider, false, nil)
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
		result.LiveData = []byte(yamlData)
		result.LiveState = []byte(yamlDataForLiveState)
		result.BridgeState = w.BuildBridgeState(payload, retrievedObjects)
		return wctx.SendStatus(result)
	}

	log.Log.Info("✅ Successfully retrieved resources", "count", len(retrievedObjects))

	result := common.NewActionResult(
		api.ActionStatusCompleted,
		api.ActionResultRefreshAndDrifted,
		fmt.Sprintf("Retrieved %d resources successfully at %s", len(retrievedObjects), time.Now().Format(time.RFC3339)),
	)
	result.Data = patched
	result.LiveData = []byte(yamlData)
	result.LiveState = []byte(yamlDataForLiveState)
	result.BridgeState = w.BuildBridgeState(payload, retrievedObjects)
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

	k8sclient, err := KubernetesClientFactory(kubeContext)
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
		retrievedObjects, err = GetLiveObjects(wctx.Context(), k8sclient, objects, false, false)
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
	cleanedObjects := cleanup.ExtraCleanupObjects(retrievedObjects)
	yamlForData, err := ObjectsToYAML(cleanedObjects)
	if err != nil {
		log.Log.Error(err, "Failed to convert objects to YAML for Data")
		return lib.SafeSendStatus(wctx, common.NewActionResult(
			api.ActionStatusFailed,
			api.ActionResultImportFailed,
			fmt.Sprintf("Failed to convert data objects to YAML: %v", err),
		), err)
	}

	log.Log.Info("📦 Import completed", "objects", len(retrievedObjects))

	result := common.NewActionResult(
		api.ActionStatusCompleted,
		api.ActionResultImportCompleted,
		fmt.Sprintf("Imported %d resources successfully at %s", len(retrievedObjects), time.Now().Format(time.RFC3339)),
	)
	result.Data = []byte(yamlForData)
	result.LiveData = []byte(yamlForData)
	result.LiveState = []byte(yamlForLiveState)
	result.BridgeState = w.BuildBridgeState(payload, retrievedObjects)
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

	result := applier.Destroy(wctx, objects)
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
	// In dry run mode, resources were not actually deleted, so skip waiting.
	if payload.DryRun {
		log.Log.Info("🔍 Dry run mode - skipping destroy watch, resources were not deleted")
		return wctx.SendStatus(common.NewActionResult(
			api.ActionStatusCompleted,
			api.ActionResultDestroyCompleted,
			"Dry run completed - resources validated but not destroyed",
		))
	}

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

	waitResult := applier.WaitForDestroy(wctx, objects, timeout)
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

	// Build LiveData from remaining objects (if any)
	var liveDataData []byte
	if len(waitResult.LiveObjects) > 0 {
		cleanedObjects := cleanup.CleanupObjects(waitResult.LiveObjects)
		yamlData, err := ObjectsToYAML(cleanedObjects)
		if err != nil {
			log.Log.Error(err, "Failed to convert remaining objects to YAML")
		} else {
			liveDataData = []byte(yamlData)
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
	}

	result := common.NewActionResult(
		api.ActionStatusCompleted,
		api.ActionResultDestroyCompleted,
		fmt.Sprintf("Destroyed resources successfully at %s", time.Now().Format(time.RFC3339)),
	)
	result.LiveData = liveDataData
	result.LiveState = []byte(yamlDataForLiveState)
	// Persist inventory for remaining resources; empty if all destroyed
	result.BridgeState = w.BuildBridgeState(payload, waitResult.LiveObjects)

	return wctx.SendStatus(result)
}

// buildInventoryInfo creates inventory info for BridgeState serialization.
// The inventory ConfigMap is an in-memory artifact (never applied to the cluster),
// so the name is a fixed constant — only the ID matters for tracking.
func buildInventoryInfo(spaceID string, unitSlug string) *SimpleInventoryInfo {
	return &SimpleInventoryInfo{
		namespace: DefaultNamespace,
		name:      InventoryConfigMapName,
		id:        fmt.Sprintf("%s-%s", spaceID, unitSlug),
	}
}

// BuildBridgeState generates the CLIUtils inventory ConfigMap YAML for the
// given applied objects (e.g., ArgoCD Application CRs, Flux Kustomization CRs).
// The result is suitable for storing in ActionResult.BridgeState.
func (w *KubernetesBridgeWorker) BuildBridgeState(payload api.BridgeWorkerPayload, appliedObjects []*unstructured.Unstructured) []byte {
	if w.applierType != CLIUtilsSSA || len(appliedObjects) == 0 {
		return nil
	}
	invInfo := buildInventoryInfo(payload.SpaceID.String(), payload.UnitSlug)
	inventoryCM := NewInventoryConfigMapWithOptions(invInfo, InventoryOptions{
		SpaceID:  payload.SpaceID.String(),
		UnitSlug: payload.UnitSlug,
	})
	objRefs := object.UnstructuredSetToObjMetadataSet(appliedObjects)
	if err := UpdateInventoryConfigMap(inventoryCM, objRefs); err != nil {
		log.Log.Error(err, "Failed to build inventory ConfigMap for BridgeState")
		return nil
	}
	invYAML, err := MarshalInventoryConfigMap(inventoryCM)
	if err != nil {
		log.Log.Error(err, "Failed to marshal inventory ConfigMap for BridgeState")
		return nil
	}
	return invYAML
}

// Finalize implements api.BridgeWorker.Finalize
// This method is called when the worker is being shutdown or cleaned up
func (w *KubernetesBridgeWorker) Finalize(wctx api.BridgeWorkerContext, payload api.BridgeWorkerPayload) error {
	// No cleanup needed for stateless applier
	log.Log.Info("Finalizing Kubernetes bridge worker")
	return nil
}
