// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package impl

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/cenkalti/backoff/v5"
	"github.com/cockroachdb/errors"
	ssautil "github.com/fluxcd/pkg/ssa/utils"
	"github.com/gosimple/slug"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/cli-utils/pkg/inventory"
	"sigs.k8s.io/cli-utils/pkg/object"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/confighub/sdk/bridge-worker/api"
	"github.com/confighub/sdk/bridge-worker/lib"
	"github.com/confighub/sdk/configkit/k8skit"
	"github.com/confighub/sdk/configkit/yamlkit"
	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/confighub/sdk/third_party/gaby"
	"github.com/confighub/sdk/workerapi"
)

type KubernetesBridgeWorker struct {
	cfg          *rest.Config
	applier      K8sApplier // Deprecated: for backward compatibility, will be removed
	applierType  ApplierName
	applierCache sync.Map // key: unit ID (string), value: K8sApplier
}

var _ api.BridgeWorker = (*KubernetesBridgeWorker)(nil)
var _ api.WatchableWorker = (*KubernetesBridgeWorker)(nil)

// NewKubernetesBridgeWorker creates a new KubernetesBridgeWorker with CLI Utils SSA as default
func NewKubernetesBridgeWorker() *KubernetesBridgeWorker {
	defaultApplier := FluxSSA
	if os.Getenv("CONFIGHUB_USE_CLIUTILS_APPLIER") == "true" ||
		os.Getenv("CONFIGHUB_USE_CLIUTILS_APPLIER") == "1" {
		defaultApplier = CLIUtilsSSA
	}
	return &KubernetesBridgeWorker{
		applierType: defaultApplier, // Default to CLI Utils SSA for inventory support
	}
}

type KubernetesWorkerParams struct {
	KubeContext string `json:",omitempty"`
	WaitTimeout string `json:",omitempty"` // Duration string like "5m0s", "10h5m"
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

func (p KubernetesWorkerParams) ToMap() map[string]interface{} {
	var result map[string]interface{}
	data, _ := json.Marshal(p)
	_ = json.Unmarshal(data, &result)
	return result
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
		targetName := os.Getenv("IN_CLUSTER_TARGET_NAME")
		if targetName == "" {
			targetName = opts.Slug
		}
		return api.BridgeWorkerInfo{
			SupportedConfigTypes: []*api.ConfigType{
				{
					ToolchainType: toolchain,
					ProviderType:  provider,
					AvailableTargets: []api.Target{
						{
							Name: targetName,
							Params: KubernetesWorkerParams{
								WaitTimeout: "2m0s",
							}.ToMap(),
						},
					},
				},
			},
		}
	}

	// Create targets for each context
	// TODO: will workerSlug + contextName be unique enough?
	var targets []api.Target
	for contextName := range kubeConfig.Contexts {
		targets = append(targets, api.Target{
			Name: fmt.Sprintf("%s-%s", opts.Slug, slug.Make(contextName)),
			Params: KubernetesWorkerParams{
				KubeContext: contextName,
				WaitTimeout: "2m0s",
			}.ToMap(),
		})
	}

	return api.BridgeWorkerInfo{
		SupportedConfigTypes: []*api.ConfigType{
			{
				ToolchainType:    toolchain,
				ProviderType:     provider,
				AvailableTargets: targets,
			},
		},
	}
}

func (w *KubernetesBridgeWorker) Apply(wctx api.BridgeWorkerContext, payload api.BridgeWorkerPayload) error {
	applier, err := w.getOrCreateApplier(payload)
	if err != nil {
		return lib.SafeSendStatus(wctx, newActionResult(
			api.ActionStatusFailed,
			api.ActionResultApplyFailed,
			err.Error(),
		), err)
	}

	objects, err := parseObjects(payload.Data)
	if err != nil {
		return lib.SafeSendStatus(wctx, newActionResult(
			api.ActionStatusFailed,
			api.ActionResultApplyFailed,
			err.Error(),
		), err)
	}

	if err := wctx.SendStatus(newActionResult(
		api.ActionStatusProgressing,
		api.ActionResultNone,
		"Starting to apply resources...",
	)); err != nil {
		return err
	}

	log.Log.Info("🔄 Applying resources...", "count", len(objects))
	if err := wctx.SendStatus(newActionResult(
		api.ActionStatusProgressing,
		api.ActionResultNone,
		"Applying resources...",
	)); err != nil {
		return err
	}

	result := applier.Apply(wctx.Context(), objects)
	if result.Error != nil {
		return lib.SafeSendStatus(wctx, newActionResult(
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

	// Don't send LiveState here - it's just the input LiveState, not the actual state
	// The real LiveState will be sent after WaitForApply when we have live resources
	actionResult := newActionResult(
		api.ActionStatusProgressing,
		api.ActionResultNone,
		"Resources applied successfully",
	)
	// Don't set LiveState - let backend keep using its current state

	return wctx.SendStatus(actionResult)
}

func objectsToYAML(objects []*unstructured.Unstructured) (string, error) {
	yamlData, err := ssautil.ObjectsToYAML(objects)
	if err != nil {
		return "", err
	}
	// ObjectsToYAML adds a trailing doc separator. Remove it.
	return gaby.NormalizeYAML(yamlData), err
}

// createApplierConfig creates an ApplierConfig from the payload
func createApplierConfig(payload api.BridgeWorkerPayload) (ApplierConfig, error) {
	_, kubeContext, err := parseTargetParams(payload)
	if err != nil {
		return ApplierConfig{}, fmt.Errorf("failed to parse target params: %v", err)
	}

	return ApplierConfig{
		KubeContext: kubeContext,
		LiveState:   payload.LiveState,
		SpaceID:     payload.SpaceID.String(),
		UnitSlug:    payload.UnitSlug,
	}, nil
}

// getOrCreateApplier returns the applier instance, creating it if needed
func (w *KubernetesBridgeWorker) getOrCreateApplier(payload api.BridgeWorkerPayload) (K8sApplier, error) {
	// Default to CLIUtilsSSA if applierType is not set (defensive programming)
	if w.applierType == "" {
		w.applierType = FluxSSA
		log.Log.Info("⚠️ Applier type was not set, defaulting to CLIUtilsSSA")
	}

	// For CLI Utils SSA, use cached applier per unit ID
	if w.applierType == CLIUtilsSSA {
		unitID := payload.UnitID.String()

		// Check cache first
		if unitID != "" {
			if cached, ok := w.applierCache.Load(unitID); ok {
				log.Log.Info("📦 Using cached applier for unit", "unitID", unitID)
				return cached.(K8sApplier), nil
			}
		}

		// Create new applier if not cached
		applierConfig, err := createApplierConfig(payload)
		if err != nil {
			return nil, err
		}
		applier, err := NewK8sApplier(CLIUtilsSSA, applierConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create applier: %v", err)
		}

		// Cache the applier for future use
		if unitID != "" {
			w.applierCache.Store(unitID, applier)
			log.Log.Info("📦 Cached new applier for unit", "unitID", unitID)
		}

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
		applierType = FluxSSA
		log.Log.Info("⚠️ Applier type was empty, using FluxSSA as fallback")
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
	workerParams, _, err := parseTargetParams(payload)
	if err != nil {
		// if we can't parse the target params, we cannot look for the resources
		return backoff.Permanent(lib.SafeSendStatus(wctx, newActionResult(
			api.ActionStatusFailed,
			api.ActionResultApplyWaitFailed,
			err.Error(),
		), err))
	}

	applier, err := w.getOrCreateApplier(payload)
	if err != nil {
		return lib.SafeSendStatus(wctx, newActionResult(
			api.ActionStatusFailed,
			api.ActionResultApplyWaitFailed,
			err.Error(),
		), err)
	}

	objects, err := parseObjects(payload.Data)
	if err != nil {
		// if we can't parse the objects, we can't wait for them
		return backoff.Permanent(lib.SafeSendStatus(wctx, newActionResult(
			api.ActionStatusFailed,
			api.ActionResultApplyWaitFailed,
			err.Error(),
		), err))
	}

	if err := wctx.SendStatus(newActionResult(
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
		log.Log.Error(waitResult.Error, "Failed to wait for resources")
		if errors.Is(waitResult.Error, context.DeadlineExceeded) {
			// log the error but don't return it
			lib.SafeSendStatus(wctx, newActionResult(
				api.ActionStatusProgressing,
				api.ActionResultNone,
				fmt.Sprintf("Failed to wait for resources: %v", waitResult.Error),
			), waitResult.Error)
			// set retry back to initial interval 30s
			return backoff.RetryAfter(30)
		}
		return lib.SafeSendStatus(wctx, newActionResult(
			api.ActionStatusFailed,
			api.ActionResultApplyWaitFailed,
			waitResult.Error.Error(),
		), waitResult.Error)
	}

	yamlData, err := objectsToYAML(waitResult.LiveObjects)
	if err != nil {
		log.Log.Error(err, "Failed to convert objects to YAML")
		return lib.SafeSendStatus(wctx, newActionResult(
			api.ActionStatusFailed,
			api.ActionResultApplyWaitFailed,
			fmt.Sprintf("Failed to convert objects to YAML: %v", err),
		), err)
	}

	// Handle inventory in LiveState for CLI Utils applier
	liveStateData := []byte(yamlData)
	if w.applierType == CLIUtilsSSA {
		// Update LiveState with inventory for CLI Utils SSA applier
		liveStateData, err = w.updateLiveStateWithInventory(wctx.Context(), payload, waitResult.LiveObjects, []byte(yamlData))
		if err != nil {
			log.Log.Error(err, "⚠️ Failed to update LiveState with inventory, using raw YAML")
			liveStateData = []byte(yamlData)
		}
	}

	status := newActionResult(
		api.ActionStatusCompleted,
		api.ActionResultApplyCompleted,
		fmt.Sprintf("Applied %d resources successfully at %s", len(waitResult.LiveObjects), time.Now().Format(time.RFC3339)),
	)
	status.LiveState = liveStateData

	// Clean up cached applier after successful WatchForApply
	unitID := payload.UnitID.String()
	if unitID != "" && w.applierType == CLIUtilsSSA {
		w.applierCache.Delete(unitID)
		log.Log.Info("🧹 Cleaned up cached applier after WatchForApply", "unitID", unitID)
	}

	wctx.SendStatus(status)
	return nil
}

// updateLiveStateWithInventory updates the LiveState to include inventory ConfigMap as the first document
func (w *KubernetesBridgeWorker) updateLiveStateWithInventory(ctx context.Context, payload api.BridgeWorkerPayload, liveObjects []*unstructured.Unstructured, yamlResources []byte) ([]byte, error) {
	// Create inventory info
	invInfo := &SimpleInventoryInfo{
		namespace: "default",
		name:      "confighub-inventory",
		id:        fmt.Sprintf("%s-%s", payload.SpaceID.String(), payload.UnitSlug),
	}

	// Create a new inventory client for this operation
	invClient := NewInMemInventoryClient()

	// Extract existing inventory ConfigMap from LiveState if present
	var inventoryCM *InventoryConfigMap
	if len(payload.LiveState) > 0 {
		_, existingCM, _, err := CreateInventoryFromLiveState(ctx, payload.LiveState, invInfo)
		if err != nil {
			log.Log.Error(err, "Failed to extract inventory from LiveState")
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

	// Save inventory to LiveState
	liveState, err := SaveInventoryToLiveState(invClient, inventoryCM, invInfo, yamlResources)
	if err != nil {
		return nil, fmt.Errorf("failed to save inventory to LiveState: %w", err)
	}

	log.Log.Info("📦 Updated LiveState with inventory", "inventoryID", invInfo.GetID(), "objects", len(objRefs))
	return liveState, nil
}

func (w *KubernetesBridgeWorker) Refresh(wctx api.BridgeWorkerContext, payload api.BridgeWorkerPayload) error {
	applier, err := w.getOrCreateApplier(payload)
	if err != nil {
		return lib.SafeSendStatus(wctx, newActionResult(
			api.ActionStatusFailed,
			api.ActionResultRefreshFailed,
			err.Error(),
		), err)
	}

	objects, err := parseObjects(payload.Data)
	if err != nil {
		return lib.SafeSendStatus(wctx, newActionResult(
			api.ActionStatusFailed,
			api.ActionResultRefreshFailed,
			err.Error(),
		), err)
	}

	if err := wctx.SendStatus(newActionResult(
		api.ActionStatusProgressing,
		api.ActionResultNone,
		"Starting to retrieve resources...",
	)); err != nil {
		return err
	}

	log.Log.Info("🔄 Retrieving resources...", "count", len(objects))
	if err := wctx.SendStatus(newActionResult(
		api.ActionStatusProgressing,
		api.ActionResultNone,
		"Retrieving resources...",
	)); err != nil {
		return err
	}

	retrievedObjects, err := applier.Refresh(wctx.Context(), objects)
	if err != nil {
		return lib.SafeSendStatus(wctx, newActionResult(
			api.ActionStatusFailed,
			api.ActionResultRefreshFailed,
			err.Error(),
		), err)
	}

	yamlData, err := objectsToYAML(extraCleanupObjects(retrievedObjects))
	if err != nil {
		log.Log.Error(err, "Failed to convert objects to YAML")
		return lib.SafeSendStatus(wctx, newActionResult(
			api.ActionStatusFailed,
			api.ActionResultRefreshFailed,
			fmt.Sprintf("Failed to convert objects to YAML: %v", err),
		), err)
	}

	// Extract inventory from LiveState if present and preserve it
	inventoryCM, _, err := SplitInventoryFromLiveState(payload.LiveState)
	if err != nil {
		log.Log.Error(err, "Failed to split inventory from LiveState, continuing without inventory")
	}

	// TODO: Known issue - namespace handling is not optimal here.
	// When we split inventory from LiveState, we may lose namespace context
	// for resources that don't explicitly specify a namespace in their YAML.
	// This could lead to resources being applied to the wrong namespace.
	// Future improvement: Ensure namespace is properly preserved during split.
	//
	// Perform diff patch on resources only (without inventory) to detect drift
	//
	// Note: We could try to copy comments from the Data to the LiveState resources:
	// https://github.com/kubernetes-sigs/kustomize/blob/master/kyaml/comments/comments.go
	// But we'd still want to detect drift.
	patched, drifted, err := yamlkit.DiffPatchWithOptions(payload.Data, []byte(yamlData), payload.Data, k8skit.K8sResourceProvider, false)
	if err != nil {
		log.Log.Error(err, "Failed to diff patch")
		return lib.SafeSendStatus(wctx, newActionResult(
			api.ActionStatusFailed,
			api.ActionResultRefreshFailed,
			fmt.Sprintf("Failed to diff patch: %v", err),
		), err)
	}

	if !drifted {
		log.Log.Info("✅ No drift detected")
		result := newActionResult(
			api.ActionStatusCompleted,
			api.ActionResultRefreshAndNoDrift,
			"Live state matches - no drift detected",
		)
		// Even when there's no drift, update LiveState with refreshed resources but preserve inventory
		if inventoryCM != nil {
			updatedLiveState, err := CombineInventoryWithResources(inventoryCM, []byte(yamlData))
			if err != nil {
				log.Log.Error(err, "Failed to combine inventory with refreshed resources")
				result.LiveState = []byte(yamlData)
			} else {
				result.LiveState = updatedLiveState
			}
		} else {
			result.LiveState = []byte(yamlData)
		}
		return wctx.SendStatus(result)
	}

	log.Log.Info("✅ Successfully retrieved resources", "count", len(retrievedObjects))

	// Combine inventory with refreshed resources for the final LiveState
	var updatedLiveState []byte
	if inventoryCM != nil {
		updatedLiveState, err = CombineInventoryWithResources(inventoryCM, []byte(yamlData))
		if err != nil {
			log.Log.Error(err, "Failed to combine inventory with refreshed resources")
			updatedLiveState = []byte(yamlData)
		}
	} else {
		updatedLiveState = []byte(yamlData)
	}

	result := newActionResult(
		api.ActionStatusCompleted,
		api.ActionResultRefreshAndDrifted,
		fmt.Sprintf("Retrieved %d resources successfully at %s", len(retrievedObjects), time.Now().Format(time.RFC3339)),
	)
	result.Data = patched
	result.LiveState = updatedLiveState // Use LiveState with preserved inventory
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

	_, kubeContext, err := parseTargetParams(payload)
	if err != nil {
		return lib.SafeSendStatus(wctx, newActionResult(
			api.ActionStatusFailed,
			api.ActionResultImportFailed,
			err.Error(),
		), err)
	}

	k8sclient, man, err := kubernetesClientFactory(kubeContext)
	if err != nil {
		return lib.SafeSendStatus(wctx, newActionResult(
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
		if err := wctx.SendStatus(newActionResult(
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
			gvk, err := parseGroupVersionKind(resourceInfo.ResourceType)
			if err != nil {
				return lib.SafeSendStatus(wctx, newActionResult(
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
		if err := wctx.SendStatus(newActionResult(
			api.ActionStatusProgressing,
			api.ActionResultNone,
			"Retrieving live state of resources...",
		)); err != nil {
			return err
		}

		retrievedObjects, err = getLiveObjects(wctx, man, objects, true)
		if err != nil {
			log.Log.Error(err, "Failed to retrieve live objects")
			return lib.SafeSendStatus(wctx, newActionResult(
				api.ActionStatusFailed,
				api.ActionResultImportFailed,
				fmt.Sprintf("Failed to retrieve live objects: %v", err),
			), err)
		}
	} else {
		// New flow: Fetch resources from cluster using parameters
		if err := wctx.SendStatus(newActionResult(
			api.ActionStatusProgressing,
			api.ActionResultNone,
			"Fetching resources from Kubernetes cluster...",
		)); err != nil {
			return err
		}

		retrievedObjects, err = w.getResourcesFromImportSource(k8sclient, importRequest)
		if err != nil {
			return lib.SafeSendStatus(wctx, newActionResult(
				api.ActionStatusFailed,
				api.ActionResultImportFailed,
				fmt.Sprintf("Failed to get cluster resources: %v", err),
			), err)
		}
	}

	if err := wctx.SendStatus(newActionResult(
		api.ActionStatusProgressing,
		api.ActionResultNone,
		"Converting resources to YAML format...",
	)); err != nil {
		return err
	}

	yamlForLiveState, err := objectsToYAML(retrievedObjects)
	if err != nil {
		log.Log.Error(err, "Failed to convert objects to YAML for live state")
		return lib.SafeSendStatus(wctx, newActionResult(
			api.ActionStatusFailed,
			api.ActionResultImportFailed,
			fmt.Sprintf("Failed to convert live state objects to YAML: %v", err),
		), err)
	}

	//heuristic extra cleanup setups for objects to make them suitable for being unit.Data
	yamlForData, err := objectsToYAML(extraCleanupObjects(retrievedObjects))
	if err != nil {
		log.Log.Error(err, "Failed to convert objects to YAML for data")
		return lib.SafeSendStatus(wctx, newActionResult(
			api.ActionStatusFailed,
			api.ActionResultImportFailed,
			fmt.Sprintf("Failed to convert data objects to YAML: %v", err),
		), err)
	}

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
		result := newActionResult(
			api.ActionStatusCompleted,
			api.ActionResultImportCompleted,
			fmt.Sprintf("Imported %d resources successfully at %s (inventory tracking failed but resources imported)", len(retrievedObjects), time.Now().Format(time.RFC3339)),
		)
		result.Data = []byte(yamlForData)
		result.LiveState = []byte(yamlForLiveState)
		return wctx.SendStatus(result)
	}

	inv.SetObjectRefs(objRefs)
	err = invClient.CreateOrUpdate(wctx.Context(), inv, inventory.UpdateOptions{})
	if err != nil {
		log.Log.Error(err, "Failed to update inventory with imported objects")
		// Continue without inventory tracking
		result := newActionResult(
			api.ActionStatusCompleted,
			api.ActionResultImportCompleted,
			fmt.Sprintf("Imported %d resources successfully at %s (inventory update failed but resources imported)", len(retrievedObjects), time.Now().Format(time.RFC3339)),
		)
		result.Data = []byte(yamlForData)
		result.LiveState = []byte(yamlForLiveState)
		return wctx.SendStatus(result)
	}

	// Save inventory to LiveState
	liveStateWithInventory, err := SaveInventoryToLiveState(invClient, inventoryCM, &invInfo, []byte(yamlForLiveState))
	if err != nil {
		log.Log.Error(err, "Failed to save inventory to LiveState")
		// Continue without inventory in LiveState
		result := newActionResult(
			api.ActionStatusCompleted,
			api.ActionResultImportCompleted,
			fmt.Sprintf("Imported %d resources successfully at %s (inventory not saved to LiveState but resources imported)", len(retrievedObjects), time.Now().Format(time.RFC3339)),
		)
		result.Data = []byte(yamlForData)
		result.LiveState = []byte(yamlForLiveState)
		return wctx.SendStatus(result)
	}

	log.Log.Info("📦 Created inventory for imported resources", "inventoryID", invInfo.id, "objects", len(objRefs))

	result := newActionResult(
		api.ActionStatusCompleted,
		api.ActionResultImportCompleted,
		fmt.Sprintf("Imported %d resources successfully at %s", len(retrievedObjects), time.Now().Format(time.RFC3339)),
	)
	result.Data = []byte(yamlForData)
	result.LiveState = liveStateWithInventory
	return wctx.SendStatus(result)
}

func (w *KubernetesBridgeWorker) Destroy(wctx api.BridgeWorkerContext, payload api.BridgeWorkerPayload) error {
	applier, err := w.getOrCreateApplier(payload)
	if err != nil {
		return lib.SafeSendStatus(wctx, newActionResult(
			api.ActionStatusFailed,
			api.ActionResultDestroyFailed,
			err.Error(),
		), err)
	}

	objects, err := parseObjects(payload.Data)
	if err != nil {
		return lib.SafeSendStatus(wctx, newActionResult(
			api.ActionStatusFailed,
			api.ActionResultDestroyFailed,
			err.Error(),
		), err)
	}

	if err = wctx.SendStatus(newActionResult(
		api.ActionStatusProgressing,
		api.ActionResultNone,
		"Starting to destroy resources...",
	)); err != nil {
		return err
	}

	result := applier.Destroy(wctx.Context(), objects)
	if result.Error != nil {
		return lib.SafeSendStatus(wctx, newActionResult(
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
	workerParams, _, err := parseTargetParams(payload)
	if err != nil {
		return lib.SafeSendStatus(wctx, newActionResult(
			api.ActionStatusFailed,
			api.ActionResultDestroyWaitFailed,
			err.Error(),
		), err)
	}

	applier, err := w.getOrCreateApplier(payload)
	if err != nil {
		return lib.SafeSendStatus(wctx, newActionResult(
			api.ActionStatusFailed,
			api.ActionResultDestroyWaitFailed,
			err.Error(),
		), err)
	}

	objects, err := parseObjects(payload.Data)
	if err != nil {
		return lib.SafeSendStatus(wctx, newActionResult(
			api.ActionStatusFailed,
			api.ActionResultDestroyWaitFailed,
			err.Error(),
		), err)
	}

	if err = wctx.SendStatus(newActionResult(
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
		return lib.SafeSendStatus(wctx, newActionResult(
			api.ActionStatusFailed,
			api.ActionResultDestroyWaitFailed,
			waitResult.Error.Error(),
		), waitResult.Error)
	}

	// For CLIUtils applier, use the LiveState from LiveStateBuilder
	// For other appliers, build LiveState from the returned LiveObjects
	var liveStateData []byte
	if w.applierType == CLIUtilsSSA {
		// CLIUtils already built the LiveState with LiveStateBuilder
		// Convert LiveObjects to YAML and add inventory
		if len(waitResult.LiveObjects) > 0 {
			yamlData, err := objectsToYAML(waitResult.LiveObjects)
			if err != nil {
				log.Log.Error(err, "Failed to convert remaining objects to YAML")
				liveStateData = []byte{}
			} else {
				// Add inventory for remaining resources
				liveStateData, err = w.updateLiveStateWithInventory(wctx.Context(), payload, waitResult.LiveObjects, []byte(yamlData))
				if err != nil {
					log.Log.Error(err, "Failed to update LiveState with inventory")
					liveStateData = []byte(yamlData)
				}
			}
		} else {
			// All resources destroyed - return empty LiveState
			liveStateData = []byte{}
		}
	} else {
		// For other appliers, check if anything remains
		if len(waitResult.LiveObjects) > 0 {
			yamlData, err := objectsToYAML(waitResult.LiveObjects)
			if err != nil {
				log.Log.Error(err, "Failed to convert remaining objects to YAML")
				liveStateData = []byte{}
			} else {
				liveStateData = []byte(yamlData)
			}
		} else {
			liveStateData = []byte{}
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

	result := newActionResult(
		api.ActionStatusCompleted,
		api.ActionResultDestroyCompleted,
		fmt.Sprintf("Destroyed resources successfully at %s", time.Now().Format(time.RFC3339)),
	)
	result.LiveState = liveStateData

	// Clean up cached applier after successful WatchForDestroy
	unitID := payload.UnitID.String()
	if unitID != "" && w.applierType == CLIUtilsSSA {
		w.applierCache.Delete(unitID)
		log.Log.Info("🧹 Cleaned up cached applier after WatchForDestroy", "unitID", unitID)
	}

	return wctx.SendStatus(result)
}

func (w *KubernetesBridgeWorker) Finalize(wctx api.BridgeWorkerContext, payload api.BridgeWorkerPayload) error {
	if err := wctx.SendStatus(newActionResult(
		api.ActionStatusProgressing,
		api.ActionResultNone,
		"Starting finalization...",
	)); err != nil {
		return err
	}

	log.Log.Info("✅ Finalization completed successfully")

	result := newActionResult(
		api.ActionStatusCompleted,
		api.ActionResultNone,
		"Finalization completed successfully",
	)
	return wctx.SendStatus(result)
}
