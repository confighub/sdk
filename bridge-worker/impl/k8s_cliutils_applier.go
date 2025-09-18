// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package impl

import (
	"context"
	"fmt"
	"time"

	"github.com/confighub/sdk/configkit/k8skit"
	ssautil "github.com/fluxcd/pkg/ssa/utils"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/kubectl/pkg/cmd/util"
	"sigs.k8s.io/cli-utils/pkg/apply"
	"sigs.k8s.io/cli-utils/pkg/apply/event"
	"sigs.k8s.io/cli-utils/pkg/common"
	"sigs.k8s.io/cli-utils/pkg/inventory"
	"sigs.k8s.io/cli-utils/pkg/object"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// Constants for annotations and labels
const (
	SpaceIDAnnotation  = "confighub.com/SpaceID"
	UnitSlugAnnotation = "confighub.com/UnitSlug"
	// Using a nil UUID (all zeros) as the default when SpaceID is not provided
	// This is a valid UUID format that won't cause parsing errors
	DefaultSpaceID       = "00000000-0000-0000-0000-000000000000"
	DefaultUnitSlug      = "default"
	DefaultInventoryName = "inventory"
	DefaultInventoryID   = "00000000-0000-0000-0000-000000000000-default"
	DefaultNamespace     = "default"
	FieldManager         = "confighub-bridge-worker"
	DefaultTimeout       = 5 * time.Minute
	PollInterval         = 2 * time.Second
	InventoryPrefix      = "inventory"
)

// SimpleInventoryInfo implements the inventory.Info interface
type SimpleInventoryInfo struct {
	namespace string
	name      string
	id        string
}

func (s *SimpleInventoryInfo) GetNamespace() string {
	return s.namespace
}

func (s *SimpleInventoryInfo) GetName() string {
	return s.name
}

func (s *SimpleInventoryInfo) GetID() inventory.ID {
	return inventory.ID(s.id)
}

// SimpleRESTClientGetter implements genericclioptions.RESTClientGetter using our existing REST config
type SimpleRESTClientGetter struct {
	restConfig      *rest.Config
	discoveryClient discovery.CachedDiscoveryInterface
	restMapper      meta.RESTMapper
}

func (r *SimpleRESTClientGetter) ToRESTConfig() (*rest.Config, error) {
	return r.restConfig, nil
}

func (r *SimpleRESTClientGetter) ToDiscoveryClient() (discovery.CachedDiscoveryInterface, error) {
	if r.discoveryClient == nil {
		discoveryClient, err := discovery.NewDiscoveryClientForConfig(r.restConfig)
		if err != nil {
			return nil, err
		}
		r.discoveryClient = memory.NewMemCacheClient(discoveryClient)
	}
	return r.discoveryClient, nil
}

func (r *SimpleRESTClientGetter) ToRESTMapper() (meta.RESTMapper, error) {
	if r.restMapper == nil {
		discoveryClient, err := r.ToDiscoveryClient()
		if err != nil {
			return nil, err
		}
		r.restMapper = restmapper.NewDeferredDiscoveryRESTMapper(discoveryClient)
	}
	return r.restMapper, nil
}

func (r *SimpleRESTClientGetter) ToRawKubeConfigLoader() clientcmd.ClientConfig {
	return nil // Not needed for our use case
}

// ApplierComponents holds all components needed for apply operations
type ApplierComponents struct {
	KubernetesClient       KubernetesClient
	DynamicClient          dynamic.Interface
	RestConfig             *rest.Config
	RestMapper             meta.RESTMapper
	Applier                *apply.Applier
	Destroyer              *apply.Destroyer
	InventoryClient        inventory.Client
	InventoryInfo          inventory.Info
	ServerSideOptions      common.ServerSideOptions
	ReconcileTimeout       time.Duration
	PruneTimeout           time.Duration
	PrunePropagationPolicy metav1.DeletionPropagation
	InventoryPolicy        inventory.Policy
}

// CLIUtilsApplier implements K8sApplier using kubernetes-sigs/cli-utils
type CLIUtilsApplier struct {
	comps            *ApplierComponents
	liveState        []byte
	spaceID          string
	unitSlug         string
	inventoryCM      *InventoryConfigMap
	invInfo          inventory.Info
	applyEventCh     <-chan event.Event           // Store event channel for WatchForApply
	applyObjects     []*unstructured.Unstructured // Store objects for WatchForApply
	applyCompleted   bool                         // Flag to track if apply has been completed
	destroyEventCh   <-chan event.Event           // Store event channel for WatchForDestroy
	destroyObjects   []*unstructured.Unstructured // Store objects for WatchForDestroy
	destroyCompleted bool                         // Flag to track if destroy has been completed
	liveStateBuilder *LiveStateBuilder            // Optimized LiveState builder
	lastResourceSet  ResourceSet                  // Store the last ResourceSet for retrieval
}

// InventoryMetadata contains extracted inventory metadata
type InventoryMetadata struct {
	SpaceID       string
	UnitSlug      string
	InventoryName string
	InventoryID   string
}

// EventProcessor handles events from apply/destroy operations.
// It tracks three types of resource modifications:
// - appliedObjects: Resources that were created or updated during apply
// - prunedObjects: Resources removed during apply because they're no longer in desired state
// - deletedObjects: Resources explicitly removed during destroy operations
type EventProcessor struct {
	appliedObjects []object.ObjMetadata
	prunedObjects  []object.ObjMetadata // Removed during apply (orphaned resources)
	deletedObjects []object.ObjMetadata // Removed during destroy (intentional deletion)
	statusEvents   []event.StatusEvent
	lastError      error
}

// Apply implements K8sApplier.Apply following the CLI-Utils algorithm
func (a *CLIUtilsApplier) Apply(ctx context.Context, objects []*unstructured.Unstructured) ApplyResult {
	// Step 1: Input validation
	if err := a.validate(); err != nil {
		return ApplyResult{Error: err}
	}

	a.setDefaultNamespaces(objects)
	log.Log.Info("🚀 Starting apply operation", "count", len(objects))

	// Step 2: Manifest processing - prepare inventory ConfigMap
	// The inventory ConfigMap should be the first document in the manifests
	var inventoryObj *unstructured.Unstructured
	var invInfo inventory.Info

	if a.inventoryCM != nil && a.inventoryCM.IsValid() {
		// Use existing inventory from LiveState
		inventoryObj = a.inventoryCM.Unstructured
		invInfo = a.invInfo
		log.Log.Info("📦 Using existing inventory from LiveState", "id", a.inventoryCM.GetInventoryID())
	} else {
		// Create new inventory if none exists
		invMetadata := InventoryMetadata{
			SpaceID:  DefaultSpaceID,
			UnitSlug: DefaultUnitSlug,
		}

		// Extract metadata from the first object's annotations.
		// These annotations are expected to be present in the YAML data received
		// from the API layer (added during preprocessing or by functions).
		if len(objects) > 0 {
			annotations := objects[0].GetAnnotations()
			if spaceID := annotations[SpaceIDAnnotation]; spaceID != "" {
				invMetadata.SpaceID = spaceID
			} else {
				log.Log.Info("⚠️ SpaceID not found in annotations, using default")
			}
			if unitSlug := annotations[UnitSlugAnnotation]; unitSlug != "" {
				invMetadata.UnitSlug = unitSlug
			} else {
				log.Log.Info("⚠️ UnitSlug not found in annotations, using default")
			}
		}

		// Generate inventory name and ID
		// Normalize the unit slug to ensure it's a valid Kubernetes resource name
		normalizedSlug := k8skit.K8sResourceProvider.NormalizeName(invMetadata.UnitSlug)
		invMetadata.InventoryName = fmt.Sprintf("%s-%s", InventoryPrefix, normalizedSlug)
		if invMetadata.SpaceID != DefaultSpaceID && len(invMetadata.SpaceID) >= 8 {
			invMetadata.InventoryName = fmt.Sprintf("%s-%s-%s", InventoryPrefix, normalizedSlug, invMetadata.SpaceID[:8])
		}
		// For InventoryID, we keep the original slug to maintain consistency with existing inventories
		invMetadata.InventoryID = fmt.Sprintf("%s-%s", invMetadata.SpaceID, invMetadata.UnitSlug)

		log.Log.Info("📦 Extracted inventory metadata", "name", invMetadata.InventoryName, "id", invMetadata.InventoryID)
		inventoryObj = a.createInventoryConfigMap(invMetadata)

		// Convert to inventory.Info
		var err error
		invInfo, err = inventory.ConfigMapToInventoryInfo(inventoryObj)
		if err != nil {
			return ApplyResult{Error: fmt.Errorf("failed to convert inventory ConfigMap: %w", err)}
		}
		log.Log.Info("📦 Created new inventory ConfigMap", "id", invMetadata.InventoryID)
	}

	// Step 3: Split inventory from resources
	// According to the algorithm, inventory should be first document
	// We currently follow the algorithm for parity check
	allObjects := append([]*unstructured.Unstructured{inventoryObj}, objects...)
	// Split the inventory from the resource objects
	invObj, resourceObjects, err := inventory.SplitUnstructureds(allObjects)
	if err != nil {
		return ApplyResult{Error: fmt.Errorf("failed to split inventory: %w", err)}
	}

	// Ensure we have the correct inventory info
	if invObj != nil {
		invInfo, err = inventory.ConfigMapToInventoryInfo(invObj)
		if err != nil {
			return ApplyResult{Error: fmt.Errorf("failed to process inventory: %w", err)}
		}
	}

	// Step 4: Applier configuration is already done in initialization
	// The applier was built with inventory client in setupApplierComponents

	// Step 5: Apply Execution
	// Configure apply options following the algorithm
	applyOptions := apply.ApplierOptions{
		ServerSideOptions: common.ServerSideOptions{
			ServerSideApply: true,
			ForceConflicts:  true,
			FieldManager:    FieldManager,
		},
		ReconcileTimeout:       DefaultTimeout,
		EmitStatusEvents:       true,  // Always emit for tracking
		NoPrune:                false, // Enable pruning by default
		PruneTimeout:           DefaultTimeout,
		PrunePropagationPolicy: metav1.DeletePropagationForeground, // Foreground for progress reporting
		InventoryPolicy:        inventory.PolicyAdoptIfNoInventory,
		DryRunStrategy:         common.DryRunNone,
	}

	// Run the applier - it returns an event channel
	log.Log.Info("📋 Starting applier with inventory (non-blocking)", "namespace", invInfo.GetNamespace(), "id", invInfo.GetID())
	eventChannel := a.comps.Applier.Run(ctx, invInfo, resourceObjects, applyOptions)

	// Store event channel and objects for WatchForApply
	a.applyEventCh = eventChannel
	a.applyObjects = resourceObjects
	a.invInfo = invInfo

	// Return immediately without blocking
	// The actual event processing will happen in WatchForApply
	log.Log.Info("✅ Apply operation started, processing will continue in WatchForApply",
		"inventoryID", invInfo.GetID())

	return ApplyResult{
		// Don't return LiveState - it's just the input and not useful
		// The actual LiveState will be built and returned in WaitForApply
		LiveState: nil,
		// Return empty ResourceSet to avoid nil pointer dereference
		// The actual ResourceSet will be available in WaitForApply
		ResourceSet: NewSimpleResourceSet(),
		Error:       nil,
	}
}

// WaitForApply implements K8sApplier.WaitForApply
func (a *CLIUtilsApplier) WaitForApply(ctx context.Context, objects []*unstructured.Unstructured, timeout time.Duration) WaitResult {
	if err := a.validate(); err != nil {
		return WaitResult{Error: err}
	}

	// Check if apply has already been completed (for cached appliers)
	if a.applyCompleted {
		log.Log.Info("✅ Apply already completed for cached applier", "unitSlug", a.unitSlug)
		// Get live objects for the already applied resources
		liveObjects, err := a.getLiveObjects(ctx, a.applyObjects, true)
		if err != nil {
			return WaitResult{Error: fmt.Errorf("failed to get live objects: %w", err)}
		}
		return WaitResult{LiveObjects: liveObjects}
	}

	// Check if we have a pending apply operation
	if a.applyEventCh == nil {
		// Fallback to original behavior if no apply operation is pending
		a.setDefaultNamespaces(objects)
		waitCtx := a.createTimeoutContext(ctx, timeout)

		if err := a.waitForResourceExistence(waitCtx, objects, true); err != nil {
			return WaitResult{Error: err}
		}

		liveObjects, err := a.getLiveObjects(waitCtx, objects, true)
		if err != nil {
			return WaitResult{Error: fmt.Errorf("failed to get live objects: %w", err)}
		}

		return WaitResult{LiveObjects: liveObjects}
	}

	// Process events from the stored Apply operation
	log.Log.Info("📋 Processing apply events from stored channel")
	processor := &EventProcessor{}
	processor.processApplyEvents(a.applyEventCh)

	// Clear the stored channel and mark as completed
	a.applyEventCh = nil
	a.applyCompleted = true

	if processor.lastError != nil {
		return WaitResult{Error: fmt.Errorf("apply operation failed: %w", processor.lastError)}
	}

	// Use the new LiveStateBuilder for optimal LiveState generation
	var updatedLiveState []byte
	var resourceSet ResourceSet

	// Use optimized builder - should always be available
	var err error
	updatedLiveState, resourceSet, err = a.liveStateBuilder.BuildLiveState(
		ctx,
		a.invInfo,
		processor,
		a.liveState,
	)
	if err != nil {
		log.Log.Error(err, "Failed to build LiveState with optimizer")
		// Use previous state as fallback
		updatedLiveState = a.liveState
		resourceSet = processor.buildResourceSet()
	}

	log.Log.Info("✅ Apply operation completed",
		"applied", len(processor.appliedObjects),
		"pruned", len(processor.prunedObjects),
		"inventoryID", a.invInfo.GetID(),
		"changeSetEntries", len(resourceSet.GetEntries()))

	// Get live objects after successful apply
	liveObjects, err := a.getLiveObjects(ctx, a.applyObjects, true)
	if err != nil {
		return WaitResult{Error: fmt.Errorf("failed to get live objects: %w", err)}
	}

	// Update LiveState with the result
	a.liveState = updatedLiveState

	return WaitResult{
		LiveObjects: liveObjects,
		ResourceSet: resourceSet,
	}
}

// Refresh implements K8sApplier.Refresh
func (a *CLIUtilsApplier) Refresh(ctx context.Context, objects []*unstructured.Unstructured) ([]*unstructured.Unstructured, error) {
	if err := a.validate(); err != nil {
		return nil, err
	}

	a.setDefaultNamespaces(objects)
	// TODO: This should not return an error in the case of Not Found
	return a.getLiveObjects(ctx, objects, true)
}

// Destroy implements K8sApplier.Destroy following the CLI-Utils algorithm
func (a *CLIUtilsApplier) Destroy(ctx context.Context, objects []*unstructured.Unstructured) DestroyResult {
	log.Log.Info("🔍 Destroy called",
		"unitSlug", a.unitSlug,
		"spaceID", a.spaceID,
		"objectCount", len(objects),
		"hasInventoryCM", a.inventoryCM != nil,
		"hasInvInfo", a.invInfo != nil,
		"destroyCompleted", a.destroyCompleted,
		"hasExistingDestroyEventCh", a.destroyEventCh != nil)

	// Step 1: Input validation
	if err := a.validate(); err != nil {
		log.Log.Error(err, "❌ Destroy validation failed")
		return DestroyResult{Error: err}
	}

	log.Log.Info("🗑️ Starting destroy operation", "unitSlug", a.unitSlug)

	// Step 2: Inventory Retrieval
	// According to the algorithm, Destroy only needs the inventory ConfigMap
	// The inventory contains all managed resources to be deleted
	var invInfo inventory.Info

	if a.inventoryCM != nil && a.inventoryCM.IsValid() {
		// Use existing inventory from LiveState
		invInfo = a.invInfo
		log.Log.Info("📦 Using existing inventory from LiveState for destroy",
			"id", a.inventoryCM.GetInventoryID(),
			"unitSlug", a.unitSlug)
	} else if a.invInfo != nil {
		// Use the inventory info we already have
		invInfo = a.invInfo
		log.Log.Info("📦 Using existing inventory info for destroy",
			"id", invInfo.GetID(),
			"unitSlug", a.unitSlug)
	} else {
		// No inventory available - cannot destroy
		log.Log.Error(nil, "❌ No inventory found - cannot destroy resources",
			"unitSlug", a.unitSlug,
			"spaceID", a.spaceID)
		return DestroyResult{Error: fmt.Errorf("no inventory found - cannot destroy resources")}
	}

	// Step 3: Inventory Client Setup is already done in initialization
	log.Log.Info("🔧 Destroyer configuration",
		"DeleteTimeout", DefaultTimeout,
		"DeletePropagationPolicy", "Foreground",
		"InventoryPolicy", "AdoptIfNoInventory",
		"unitSlug", a.unitSlug)

	// Step 4: Destroyer Configuration
	destroyOptions := apply.DestroyerOptions{
		DeleteTimeout:           DefaultTimeout,
		DeletePropagationPolicy: metav1.DeletePropagationForeground, // Foreground for progress reporting
		InventoryPolicy:         inventory.PolicyAdoptIfNoInventory,
		EmitStatusEvents:        true, // Always emit for tracking
		DryRunStrategy:          common.DryRunNone,
	}

	// Step 5: Destroy Execution
	// The destroyer will:
	// a. Retrieve inventory from cluster (managed resources)
	// b. Delete resources in reverse dependency order
	// c. Wait for deletion (if timeout set)
	// d. Delete inventory ConfigMap
	log.Log.Info("📋 Starting destroyer with inventory (non-blocking)",
		"namespace", invInfo.GetNamespace(),
		"id", invInfo.GetID(),
		"unitSlug", a.unitSlug)

	eventChannel := a.comps.Destroyer.Run(ctx, invInfo, destroyOptions)

	log.Log.Info("🔄 Destroyer.Run returned event channel",
		"hasEventChannel", eventChannel != nil,
		"unitSlug", a.unitSlug)

	// Store event channel and info for WatchForDestroy
	a.destroyEventCh = eventChannel
	a.destroyObjects = objects
	a.invInfo = invInfo

	log.Log.Info("💾 Stored destroy state for WatchForDestroy",
		"hasDestroyEventCh", a.destroyEventCh != nil,
		"objectCount", len(a.destroyObjects),
		"unitSlug", a.unitSlug)

	// Return immediately without blocking
	// The actual event processing will happen in WatchForDestroy
	log.Log.Info("✅ Destroy operation started, processing will continue in WatchForDestroy",
		"inventoryID", invInfo.GetID(),
		"unitSlug", a.unitSlug)

	return DestroyResult{
		// Don't return LiveState - it's just the input and not useful
		// The actual LiveState will be built and returned in WaitForDestroy
		LiveState: nil,
		// Return empty ResourceSet to avoid nil pointer dereference
		// The actual ResourceSet will be available in WaitForDestroy
		ResourceSet: NewSimpleResourceSet(),
		Error:       nil,
	}
}

// WaitForDestroy implements K8sApplier.WaitForDestroy
func (a *CLIUtilsApplier) WaitForDestroy(ctx context.Context, objects []*unstructured.Unstructured, timeout time.Duration) WaitResult {
	log.Log.Info("🔍 WaitForDestroy called",
		"unitSlug", a.unitSlug,
		"spaceID", a.spaceID,
		"destroyCompleted", a.destroyCompleted,
		"hasDestroyEventCh", a.destroyEventCh != nil,
		"objectCount", len(objects),
		"timeout", timeout)

	if err := a.validate(); err != nil {
		log.Log.Error(err, "❌ WaitForDestroy validation failed")
		return WaitResult{Error: err}
	}

	// Check if destroy has already been completed (for cached appliers)
	if a.destroyCompleted {
		log.Log.Info("✅ Destroy already completed for cached applier",
			"unitSlug", a.unitSlug,
			"spaceID", a.spaceID)
		// Return empty LiveState since destroy is complete
		return WaitResult{
			LiveObjects: []*unstructured.Unstructured{},
			ResourceSet: a.lastResourceSet,
			Error:       nil,
		}
	}

	// Check if we have a pending destroy operation
	if a.destroyEventCh == nil {
		log.Log.Info("⚠️ No destroy event channel, falling back to waitForResourceExistence",
			"unitSlug", a.unitSlug,
			"objectCount", len(objects))
		// Fallback to original behavior if no destroy operation is pending
		a.setDefaultNamespaces(objects)
		waitCtx := a.createTimeoutContext(ctx, timeout)
		err := a.waitForResourceExistence(waitCtx, objects, false)
		if err != nil {
			log.Log.Error(err, "❌ waitForResourceExistence failed in WaitForDestroy fallback")
			return WaitResult{Error: err}
		}
		// Return empty result for fallback case
		return WaitResult{
			LiveObjects: []*unstructured.Unstructured{},
			ResourceSet: NewSimpleResourceSet(),
			Error:       nil,
		}
	}

	// Process events from the stored Destroy operation
	log.Log.Info("📋 Processing destroy events from stored channel",
		"unitSlug", a.unitSlug,
		"inventoryID", a.invInfo.GetID())
	processor := &EventProcessor{}
	processor.processDestroyEvents(a.destroyEventCh)

	// Clear the stored channel and mark as completed
	a.destroyEventCh = nil
	a.destroyCompleted = true
	log.Log.Info("🔄 Marked destroy as completed and cleared event channel",
		"unitSlug", a.unitSlug)

	if processor.lastError != nil {
		log.Log.Error(processor.lastError, "❌ Destroy processor reported error",
			"unitSlug", a.unitSlug,
			"deletedCount", len(processor.deletedObjects))
		return WaitResult{Error: fmt.Errorf("destroy operation failed: %w", processor.lastError)}
	}

	// Use the new LiveStateBuilder for optimal LiveState generation
	var updatedLiveState []byte
	var resourceSet ResourceSet

	if a.liveStateBuilder != nil {
		// After destroy, inventory should be empty or deleted
		var err error
		updatedLiveState, resourceSet, err = a.liveStateBuilder.BuildLiveState(
			ctx,
			a.invInfo,
			processor,
			a.liveState,
		)
		if err != nil {
			log.Log.Error(err, "Failed to build LiveState after destroy, using empty state")
			updatedLiveState = a.createEmptyLiveState(ctx, a.invInfo)
			resourceSet = processor.buildResourceSet()
		}
	} else {
		// Fallback to empty state
		updatedLiveState = a.createEmptyLiveState(ctx, a.invInfo)
		resourceSet = processor.buildResourceSet()
	}

	log.Log.Info("✅ Destroy operation completed",
		"deleted", len(processor.deletedObjects),
		"inventoryID", a.invInfo.GetID(),
		"unitSlug", a.unitSlug,
		"changeSetEntries", len(resourceSet.GetEntries()))

	// Update LiveState with the result
	a.liveState = updatedLiveState

	// Store the ResourceSet for retrieval
	a.lastResourceSet = resourceSet

	// Get live objects to return (should be empty or remaining resources)
	liveObjects, err := a.getLiveObjects(ctx, nil, false)
	if err != nil {
		log.Log.Error(err, "Failed to get live objects after destroy")
		liveObjects = []*unstructured.Unstructured{}
	}

	return WaitResult{
		LiveObjects: liveObjects,
		ResourceSet: resourceSet,
		Error:       nil,
	}
}

// createEmptyLiveState creates an empty LiveState after destroy
func (a *CLIUtilsApplier) createEmptyLiveState(_ context.Context, _ inventory.Info) []byte {
	// After destroy, all resources and the inventory have been deleted
	// Return empty LiveState to indicate clean state
	return []byte{}
}

// Helper methods

func (a *CLIUtilsApplier) validate() error {
	if a.comps == nil {
		return fmt.Errorf("dependencies not initialized")
	}
	if a.comps.Applier == nil {
		return fmt.Errorf("applier not initialized")
	}
	if a.comps.KubernetesClient == nil {
		return fmt.Errorf("kubernetes client not initialized")
	}
	return nil
}

func (a *CLIUtilsApplier) createInventoryConfigMap(metadata InventoryMetadata) *unstructured.Unstructured {
	// Validate metadata before creating ConfigMap
	// Possible scenarios where inventory metadata could be empty:
	// 1. Corrupted LiveState: If LiveState contains a malformed inventory ConfigMap with missing metadata fields
	// 2. Parsing Errors: When SplitInventoryFromLiveState encounters a ConfigMap without proper inventory labels/annotations
	// 3. Legacy Data: Old inventory ConfigMaps that don't follow the current naming convention
	// 4. Manual Intervention: Someone manually created an inventory ConfigMap without proper metadata
	if metadata.InventoryName == "" {
		log.Log.Error(nil, "Invalid inventory metadata: name is empty")
		metadata.InventoryName = DefaultInventoryName
	}
	if metadata.InventoryID == "" {
		log.Log.Error(nil, "Invalid inventory metadata: ID is empty")
		metadata.InventoryID = DefaultInventoryID
	}

	namespace := DefaultNamespace
	if a.comps != nil && a.comps.InventoryInfo != nil {
		namespace = a.comps.InventoryInfo.GetNamespace()
	}

	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]interface{}{
				"name":      metadata.InventoryName,
				"namespace": namespace,
				"labels": map[string]interface{}{
					InventoryIDLabel: metadata.InventoryID,
				},
				"annotations": map[string]interface{}{
					FunctionAnnotation: "inventory",
					SpaceIDAnnotation:  metadata.SpaceID,
					UnitSlugAnnotation: metadata.UnitSlug,
				},
			},
			"data": map[string]interface{}{},
		},
	}
}

func (a *CLIUtilsApplier) createTimeoutContext(ctx context.Context, timeout time.Duration) context.Context {
	if timeout <= 0 {
		return ctx
	}

	waitCtx, _ := context.WithTimeout(ctx, timeout)
	log.Log.Info("⏱️ Using timeout", "timeout", timeout.String())
	return waitCtx
}

func (a *CLIUtilsApplier) setDefaultNamespaces(objects []*unstructured.Unstructured) {
	if a.comps.KubernetesClient == nil {
		return
	}

	for _, obj := range objects {
		if obj.GetNamespace() == "" {
			if isNamespaced, err := a.comps.KubernetesClient.IsObjectNamespaced(obj); err == nil && isNamespaced {
				obj.SetNamespace("default")
			}
		}
	}
}

// waitForResourceExistence waits for resources to either exist or be deleted based on the expectExist parameter
// If expectExist is true, waits for resources to exist; if false, waits for resources to be deleted
func (a *CLIUtilsApplier) waitForResourceExistence(ctx context.Context, objects []*unstructured.Unstructured, expectExist bool) error {
	ticker := time.NewTicker(PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for resources")
		case <-ticker.C:
			allReady := true
			for _, obj := range objects {
				exists := a.resourceExists(ctx, obj)
				if expectExist && !exists {
					allReady = false
					log.Log.Info("⏳ Resource not ready yet", "name", obj.GetName())
					break
				} else if !expectExist && exists {
					allReady = false
					log.Log.Info("⏳ Resource still terminating", "name", obj.GetName())
					break
				}
			}

			if allReady {
				if expectExist {
					log.Log.Info("✅ All resources are ready")
				} else {
					log.Log.Info("✅ All resources terminated")
				}
				return nil
			}
		}
	}
}

func (a *CLIUtilsApplier) resourceExists(ctx context.Context, obj *unstructured.Unstructured) bool {
	key := client.ObjectKey{
		Namespace: obj.GetNamespace(),
		Name:      obj.GetName(),
	}
	tempObj := obj.DeepCopyObject().(*unstructured.Unstructured)
	return a.comps.KubernetesClient.Get(ctx, key, tempObj) == nil
}

func (a *CLIUtilsApplier) getLiveObjects(ctx context.Context, objects []*unstructured.Unstructured, doCleanup bool) ([]*unstructured.Unstructured, error) {
	liveObjects := make([]*unstructured.Unstructured, len(objects))
	for i, obj := range objects {
		key := client.ObjectKey{
			Namespace: obj.GetNamespace(),
			Name:      obj.GetName(),
		}
		u := obj.DeepCopyObject().(*unstructured.Unstructured)
		if err := a.comps.KubernetesClient.Get(ctx, key, u); err != nil {
			return nil, err
		}

		if doCleanup {
			cleanup(u)
		}
		liveObjects[i] = u
	}
	return liveObjects, nil
}

// saveCurrentInventoryToLiveState saves the current inventory state to LiveState
// The inventory has already been updated by CLI-Utils internally during apply/prune
func (a *CLIUtilsApplier) saveCurrentInventoryToLiveState(ctx context.Context, invInfo inventory.Info) ([]byte, error) {
	if a.comps.InventoryClient == nil {
		return a.liveState, nil
	}

	// Get the current inventory that was already updated by CLI-Utils
	inv, err := a.comps.InventoryClient.Get(ctx, invInfo, inventory.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get updated inventory: %w", err)
	}

	// Get the current object references from the updated inventory
	// This includes applied objects but excludes successfully pruned objects
	currentObjRefs := inv.GetObjectRefs()

	// Get live objects for all resources currently in the inventory
	liveObjects := make([]*unstructured.Unstructured, 0)

	// Use dynamic client with REST mapper to fetch resources
	for _, objMeta := range currentObjRefs {
		// The REST mapper can resolve the proper GVR (GroupVersionResource) for a given GroupKind
		gk := schema.GroupKind{
			Group: objMeta.GroupKind.Group,
			Kind:  objMeta.GroupKind.Kind,
		}

		// Get the REST mapping to find the resource and scope
		mapping, err := a.comps.RestMapper.RESTMapping(gk)
		if err != nil {
			log.Log.V(1).Info("Could not find REST mapping for resource",
				"groupKind", gk,
				"error", err)
			continue
		}

		// Use dynamic client with the resolved GVR
		var fetchObj *unstructured.Unstructured
		if mapping.Scope.Name() == meta.RESTScopeNameNamespace {
			// Namespaced resource
			fetchObj, err = a.comps.DynamicClient.Resource(mapping.Resource).
				Namespace(objMeta.Namespace).
				Get(ctx, objMeta.Name, metav1.GetOptions{})
		} else {
			// Cluster-scoped resource
			fetchObj, err = a.comps.DynamicClient.Resource(mapping.Resource).
				Get(ctx, objMeta.Name, metav1.GetOptions{})
		}

		if err == nil {
			cleanup(fetchObj)
			liveObjects = append(liveObjects, fetchObj)
		} else {
			// This can happen if the object was deleted externally
			log.Log.V(2).Info("Could not fetch object from cluster",
				"object", objMeta,
				"resource", mapping.Resource,
				"error", err)
		}
	}

	// Convert to YAML
	yamlData, err := ssautil.ObjectsToYAML(liveObjects)
	if err != nil {
		return nil, fmt.Errorf("failed to convert objects to YAML: %w", err)
	}

	// Ensure inventory ConfigMap is updated
	if a.inventoryCM == nil {
		a.inventoryCM = NewInventoryConfigMapWithOptions(invInfo, InventoryOptions{
			SpaceID:  a.spaceID,
			UnitSlug: a.unitSlug,
		})
	}

	// Update inventory ConfigMap with current objects from the inventory
	if err := UpdateInventoryConfigMap(a.inventoryCM, currentObjRefs); err != nil {
		return nil, fmt.Errorf("failed to update inventory ConfigMap: %w", err)
	}

	// Save inventory to LiveState
	updatedLiveState, err := SaveInventoryToLiveState(
		a.comps.InventoryClient.(*InMemInventoryClient),
		a.inventoryCM,
		invInfo,
		[]byte(yamlData),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to save inventory to LiveState: %w", err)
	}

	return updatedLiveState, nil
}

// EventProcessor methods

func (p *EventProcessor) processApplyEvents(eventChannel <-chan event.Event) {
	for ev := range eventChannel {
		switch ev.Type {
		case event.InitType:
			log.Log.Info("📋 Apply operation initialized", "actionGroups", len(ev.InitEvent.ActionGroups))

		case event.ErrorType:
			p.lastError = ev.ErrorEvent.Err
			log.Log.Error(p.lastError, "❌ Error during apply")

		case event.ApplyType:
			p.processApplyEvent(ev.ApplyEvent)

		case event.StatusType:
			p.statusEvents = append(p.statusEvents, ev.StatusEvent)
			log.Log.V(1).Info("📊 Resource status",
				"object", ev.StatusEvent.Identifier)

		case event.PruneType:
			p.processPruneEvent(ev.PruneEvent)

		case event.WaitType:
			log.Log.V(1).Info("⏳ Waiting for reconciliation",
				"object", ev.WaitEvent.Identifier)

		case event.ValidationType:
			if ev.ValidationEvent.Error != nil {
				log.Log.Error(ev.ValidationEvent.Error, "⚠️ Validation error")
			}

		case event.ActionGroupType:
			log.Log.V(1).Info("🎯 Action group event",
				"groupName", ev.ActionGroupEvent.GroupName,
				"action", ev.ActionGroupEvent.Action,
				"status", ev.ActionGroupEvent.Status)
		}
	}

	log.Log.Info("🎉 Apply completed", "applied", len(p.appliedObjects), "pruned", len(p.prunedObjects))
}

func (p *EventProcessor) processDestroyEvents(eventChannel <-chan event.Event) {
	eventCount := 0
	log.Log.Info("🔄 Starting to process destroy events from channel")

	// Add timeout to prevent hanging on unclosed channels
	timeout := time.NewTimer(90 * time.Second)
	defer timeout.Stop()

	// Track expected vs completed deletions
	var expectedDeletions int
	var inventoryProcessed bool
	lastEventTime := time.Now()

	for {
		select {
		case ev, ok := <-eventChannel:
			if !ok {
				// Channel closed normally
				log.Log.Info("📪 Event channel closed normally")
				goto done
			}

			eventCount++
			lastEventTime = time.Now()
			log.Log.Info("📨 Received destroy event",
				"eventNum", eventCount,
				"eventType", ev.Type)

			switch ev.Type {
			case event.InitType:
				log.Log.Info("📋 Destroy operation initialized",
					"actionGroups", len(ev.InitEvent.ActionGroups))
				for i, group := range ev.InitEvent.ActionGroups {
					log.Log.Info("  Action group",
						"groupNum", i,
						"name", group.Name,
						"action", group.Action,
						"identifierCount", len(group.Identifiers))

					// Count expected deletions
					if group.Action == event.DeleteAction {
						expectedDeletions += len(group.Identifiers)
					}
					// Check for inventory action (signals completion)
					if group.Name == "inventory-delete-or-update-0" || group.Action == event.InventoryAction {
						inventoryProcessed = true
					}
				}

			case event.ErrorType:
				p.lastError = ev.ErrorEvent.Err
				log.Log.Error(p.lastError, "❌ Error during destroy",
					"eventNum", eventCount)

			case event.DeleteType:
				log.Log.Info("🗑️ Processing delete event",
					"eventNum", eventCount)
				p.processDeleteEvent(ev.DeleteEvent)

			case event.WaitType:
				log.Log.Info("⏳ Waiting for termination",
					"eventNum", eventCount,
					"object", ev.WaitEvent.Identifier)

			case event.StatusType:
				p.statusEvents = append(p.statusEvents, ev.StatusEvent)
				log.Log.V(1).Info("📊 Resource status during destroy",
					"eventNum", eventCount,
					"object", ev.StatusEvent.Identifier)

			case event.ActionGroupType:
				log.Log.Info("🎯 Action group event",
					"eventNum", eventCount,
					"groupName", ev.ActionGroupEvent.GroupName,
					"action", ev.ActionGroupEvent.Action,
					"status", ev.ActionGroupEvent.Status)
				// Track when delete actions start/finish
				if ev.ActionGroupEvent.Action == event.DeleteAction {
					if ev.ActionGroupEvent.Status == event.Started {
						log.Log.Info("🚀 Delete action group started",
							"groupName", ev.ActionGroupEvent.GroupName)
					} else if ev.ActionGroupEvent.Status == event.Finished {
						log.Log.Info("✅ Delete action group finished",
							"groupName", ev.ActionGroupEvent.GroupName,
							"deleted", len(p.deletedObjects))
					}
				}
				// Check for inventory action completion
				if ev.ActionGroupEvent.Action == event.InventoryAction && ev.ActionGroupEvent.Status == event.Finished {
					inventoryProcessed = true
					log.Log.Info("📦 Inventory action completed")
				}

			case event.ValidationType:
				if ev.ValidationEvent.Error != nil {
					log.Log.Error(ev.ValidationEvent.Error, "⚠️ Validation error during destroy",
						"eventNum", eventCount)
				}

			default:
				log.Log.Info("❓ Unhandled event type",
					"eventNum", eventCount,
					"eventType", ev.Type)
			}

			// Check if we've processed all expected deletions and inventory
			if expectedDeletions > 0 && len(p.deletedObjects) >= expectedDeletions && inventoryProcessed {
				// Give it a small grace period for any final events
				gracePeriod := time.NewTimer(2 * time.Second)
				select {
				case ev2, ok := <-eventChannel:
					if ok {
						// Process any remaining events
						log.Log.Info("📨 Processing final event during grace period", "eventType", ev2.Type)
					}
					gracePeriod.Stop()
				case <-gracePeriod.C:
					log.Log.Info("✅ All expected deletions completed, ending event processing",
						"expected", expectedDeletions,
						"deleted", len(p.deletedObjects))
					goto done
				}
			}

			// Check for idle timeout (no events for 10 seconds after deletions started)
			if len(p.deletedObjects) > 0 && time.Since(lastEventTime) > 10*time.Second {
				log.Log.Info("⏱️ No events for 10 seconds, assuming completion",
					"deleted", len(p.deletedObjects))
				goto done
			}

		case <-timeout.C:
			log.Log.Info("⏱️ Destroy event processing timeout after 90 seconds",
				"eventCount", eventCount,
				"deleted", len(p.deletedObjects))
			goto done
		}
	}

done:
	log.Log.Info("🎉 Destroy event processing completed",
		"totalEvents", eventCount,
		"deleted", len(p.deletedObjects),
		"expected", expectedDeletions,
		"hasError", p.lastError != nil)
}

func (p *EventProcessor) processApplyEvent(e event.ApplyEvent) {
	if e.Error != nil {
		log.Log.Error(e.Error, "❌ Failed to apply",
			"object", e.Identifier,
			"status", e.Status)
		// Don't set lastError for non-fatal errors (e.g., already exists)
		if e.Status != event.ApplySkipped {
			p.lastError = e.Error
		}
	} else {
		log.Log.Info("✅ Applied",
			"object", e.Identifier,
			"status", e.Status)
		p.appliedObjects = append(p.appliedObjects, e.Identifier)
	}
}

// processPruneEvent handles pruning of resources that exist in the cluster but are no longer in the desired state.
// Pruning occurs during apply operations when resources tracked in inventory are no longer part of the
// configuration being applied. This is automatic cleanup of orphaned resources.
func (p *EventProcessor) processPruneEvent(e event.PruneEvent) {
	log.Log.Info("🔍 Prune event details",
		"identifier", e.Identifier,
		"status", e.Status,
		"hasError", e.Error != nil,
		"groupName", e.GroupName)

	if e.Error != nil {
		log.Log.Error(e.Error, "❌ Failed to prune",
			"object", e.Identifier,
			"status", e.Status,
			"groupName", e.GroupName)
		// Don't set lastError for non-fatal prune errors
		if e.Status != event.PruneSkipped {
			p.lastError = e.Error
			log.Log.Info("💥 Setting lastError due to prune failure",
				"status", e.Status)
		} else {
			log.Log.Info("⏭️ Prune skipped, not setting error",
				"status", e.Status)
		}
	} else {
		log.Log.Info("🗑️ Successfully pruned",
			"object", e.Identifier,
			"status", e.Status,
			"groupName", e.GroupName,
			"prunedCount", len(p.prunedObjects)+1)
		p.prunedObjects = append(p.prunedObjects, e.Identifier)
	}
}

// processDeleteEvent handles explicit deletion of resources during destroy operations.
// Delete events occur when resources are intentionally removed, either through:
// - Destroy operations that remove all resources in the inventory
// - Explicit deletion of specific resources
// Unlike pruning (which happens during apply), deletion is a deliberate removal action.
func (p *EventProcessor) processDeleteEvent(e event.DeleteEvent) {
	log.Log.Info("📊 Delete event details",
		"identifier", e.Identifier,
		"status", e.Status,
		"hasError", e.Error != nil,
		"groupName", e.GroupName)

	if e.Error != nil {
		log.Log.Error(e.Error, "❌ Failed to delete",
			"object", e.Identifier,
			"status", e.Status,
			"groupName", e.GroupName)
		// Don't set lastError for non-fatal delete errors (e.g., not found)
		if e.Status != event.DeleteSkipped {
			p.lastError = e.Error
			log.Log.Info("💥 Setting lastError due to delete failure",
				"status", e.Status)
		} else {
			log.Log.Info("⏭️ Delete skipped, not setting error",
				"status", e.Status)
		}
	} else {
		log.Log.Info("🗑️ Successfully deleted",
			"object", e.Identifier,
			"status", e.Status,
			"groupName", e.GroupName,
			"deletedCount", len(p.deletedObjects)+1)
		p.deletedObjects = append(p.deletedObjects, e.Identifier)
	}
}

func (p *EventProcessor) buildResourceSet() ResourceSet {
	resourceSet := NewSimpleResourceSet()

	for _, obj := range p.appliedObjects {
		resourceSet.Add(SimpleResourceSetEntry{
			Name:      obj.Name,
			Namespace: obj.Namespace,
			Kind:      obj.GroupKind.Kind,
			Action:    "Applied",
		})
	}

	for _, obj := range p.prunedObjects {
		resourceSet.Add(SimpleResourceSetEntry{
			Name:      obj.Name,
			Namespace: obj.Namespace,
			Kind:      obj.GroupKind.Kind,
			Action:    "Pruned",
		})
	}

	for _, obj := range p.deletedObjects {
		resourceSet.Add(SimpleResourceSetEntry{
			Name:      obj.Name,
			Namespace: obj.Namespace,
			Kind:      obj.GroupKind.Kind,
			Action:    "Deleted",
		})
	}

	return resourceSet
}

func setupApplierComponents(config ApplierConfig) (*ApplierComponents, inventory.Info, *InventoryConfigMap, error) {
	// Create default inventory info
	defaultInvInfo := &SimpleInventoryInfo{
		namespace: "default",
		name:      "confighub-inventory",
		id:        fmt.Sprintf("%s-%s", config.SpaceID, config.UnitSlug),
	}

	// Initialize Kubernetes clients
	cfg, err := kubernetesConfigFactory(config.KubeContext)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to get config: %w", err)
	}

	k8sClient, err := client.New(cfg, client.Options{})
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to create client: %w", err)
	}

	dynamicClient, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to create dynamic client: %w", err)
	}

	// Create discovery client and REST mapper for GVK resolution
	discoveryClient, err := discovery.NewDiscoveryClientForConfig(cfg)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to create discovery client: %w", err)
	}
	restMapper := restmapper.NewDeferredDiscoveryRESTMapper(memory.NewMemCacheClient(discoveryClient))

	// Create kubectl factory
	configFlags := genericclioptions.NewConfigFlags(true)
	if config.KubeContext != "" {
		configFlags.Context = &config.KubeContext
	}
	factory := util.NewFactory(configFlags)

	// Determine inventory client based on LiveState
	var invClient inventory.Client
	var invInfo inventory.Info
	var inventoryCM *InventoryConfigMap

	if len(config.LiveState) > 0 {
		log.Log.Info("📦 Using in-memory inventory from LiveState")
		invClient, inventoryCM, _, err = CreateInventoryFromLiveState(context.Background(), config.LiveState, defaultInvInfo)
		if err != nil {
			log.Log.Error(err, "⚠️ Failed to create inventory from LiveState, falling back to default")
			invClient = NewInMemInventoryClient()
			inventoryCM = NewInventoryConfigMap(defaultInvInfo)
		}
		invInfo = defaultInvInfo
	} else {
		log.Log.Info("📦 Using standard in-memory inventory")
		invClient = NewInMemInventoryClient()
		// Use default inventory info or override if SpaceID/UnitSlug provided
		if config.SpaceID != "" && config.UnitSlug != "" {
			invInfo = defaultInvInfo
			inventoryCM = NewInventoryConfigMapWithOptions(defaultInvInfo, InventoryOptions{
				SpaceID:  config.SpaceID,
				UnitSlug: config.UnitSlug,
			})
		} else {
			invInfo = &SimpleInventoryInfo{
				namespace: "default",
				name:      "confighub-inventory",
				id:        "confighub-" + config.KubeContext,
			}
			inventoryCM = NewInventoryConfigMap(invInfo)
		}

		// Initialize the inventory in the client for first-time use
		// This is crucial - the CLI-Utils applier expects the inventory to exist
		ctx := context.TODO()
		newInv, err := invClient.NewInventory(invInfo)
		if err != nil {
			log.Log.Error(err, "Failed to create initial inventory")
		} else {
			// Create an empty inventory with no objects
			newInv.SetObjectRefs(object.ObjMetadataSet{})
			if err := invClient.CreateOrUpdate(ctx, newInv, inventory.UpdateOptions{}); err != nil {
				log.Log.Error(err, "Failed to initialize inventory in client")
			} else {
				log.Log.Info("📦 Initialized empty inventory in client", "id", invInfo.GetID())
			}
		}
	}

	// Create applier
	applier, err := apply.NewApplierBuilder().
		WithFactory(factory).
		WithInventoryClient(invClient).
		Build()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to create applier: %w", err)
	}

	// Create destroyer
	destroyer, err := apply.NewDestroyerBuilder().
		WithFactory(factory).
		WithInventoryClient(invClient).
		Build()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to create destroyer: %w", err)
	}

	log.Log.Info("✅ Created applier components", "hasLiveState", len(config.LiveState) > 0)

	return &ApplierComponents{
		KubernetesClient: k8sClient,
		DynamicClient:    dynamicClient,
		RestConfig:       cfg,
		RestMapper:       restMapper,
		Applier:          applier,
		Destroyer:        destroyer,
		InventoryClient:  invClient,
		InventoryInfo:    invInfo,
	}, invInfo, inventoryCM, nil
}

// NewCLIUtilsApplier creates a new K8sApplier instance
func NewCLIUtilsApplier(config ApplierConfig) (K8sApplier, error) {
	// Setup all components with consolidated logic
	comps, invInfo, inventoryCM, err := setupApplierComponents(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create applier components: %w", err)
	}

	// Create the optimized LiveStateBuilder
	liveStateBuilder := NewLiveStateBuilder(
		comps.InventoryClient,
		comps.DynamicClient,
		comps.RestMapper,
		config.SpaceID,
		config.UnitSlug,
	)

	log.Log.Info("🚀 Created CLIUtilsApplier with LiveStateBuilder")

	return &CLIUtilsApplier{
		comps:            comps,
		liveState:        config.LiveState,
		spaceID:          config.SpaceID,
		unitSlug:         config.UnitSlug,
		inventoryCM:      inventoryCM,
		invInfo:          invInfo,
		liveStateBuilder: liveStateBuilder,
	}, nil
}
