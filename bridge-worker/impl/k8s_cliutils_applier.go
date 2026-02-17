// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package impl

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/confighub/sdk/bridge-worker/api"
	"github.com/confighub/sdk/configkit/k8skit"
	funcApi "github.com/confighub/sdk/function/api"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
	"k8s.io/kubectl/pkg/cmd/util"
	"sigs.k8s.io/cli-utils/pkg/apply"
	"sigs.k8s.io/cli-utils/pkg/apply/event"
	"sigs.k8s.io/cli-utils/pkg/common"
	"sigs.k8s.io/cli-utils/pkg/inventory"
	"sigs.k8s.io/cli-utils/pkg/kstatus/polling"
	"sigs.k8s.io/cli-utils/pkg/kstatus/polling/aggregator"
	"sigs.k8s.io/cli-utils/pkg/kstatus/polling/collector"
	pollingevent "sigs.k8s.io/cli-utils/pkg/kstatus/polling/event"
	"sigs.k8s.io/cli-utils/pkg/kstatus/status"
	"sigs.k8s.io/cli-utils/pkg/object"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	// Using a nil UUID (all zeros) as the default when SpaceID is not provided
	// This is a valid UUID format that won't cause parsing errors
	DefaultSpaceID       = "00000000-0000-0000-0000-000000000000"
	DefaultUnitSlug      = "default"
	DefaultInventoryName = "inventory"
	DefaultInventoryID   = "00000000-0000-0000-0000-000000000000-default"
	DefaultNamespace     = "default"
	FieldManager         = "confighub-bridge-worker"
	DefaultTimeout       = LargeWaitTimeout
	PollInterval         = 2 * time.Second
	InventoryPrefix      = "inventory"
)

var (
	klogInitOnce sync.Once
)

// initKlog initializes klog with verbose logging for CLI-Utils debugging
func initKlog() {
	klogInitOnce.Do(func() {
		// Initialize klog flags
		fs := flag.NewFlagSet("klog", flag.ContinueOnError)
		klog.InitFlags(fs)

		// Set verbosity level based on CONFIGHUB_DEBUG environment variable
		// Levels: 0=critical, 1=errors, 2=warnings, 3=info, 4=debug, 5=trace
		logLevel := "2" // Default to warnings
		if os.Getenv("CONFIGHUB_DEBUG") == "1" {
			logLevel = "5" // Enable detailed CLI-Utils trace logs in debug mode
			log.Log.Info("🔍 Initialized klog with verbosity level 5 for CLI-Utils debugging (CONFIGHUB_DEBUG=1)")
		} else {
			log.Log.Info("🔍 Initialized klog with verbosity level 2 (set CONFIGHUB_DEBUG=1 for detailed logs)")
		}
		_ = fs.Set("v", logLevel)
	})
}

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

// FreshDiscoveryClient is a wrapper around DiscoveryClient that implements CachedDiscoveryInterface
// but always returns fresh data (no caching)
type FreshDiscoveryClient struct {
	*discovery.DiscoveryClient
}

// Fresh implements CachedDiscoveryInterface - always returns true since we don't cache
func (f *FreshDiscoveryClient) Fresh() bool {
	return true
}

// Invalidate implements CachedDiscoveryInterface - no-op since we don't cache
func (f *FreshDiscoveryClient) Invalidate() {
	// No-op: we always fetch fresh data
}

// SimpleRESTClientGetter implements genericclioptions.RESTClientGetter using our existing REST config
type SimpleRESTClientGetter struct {
	restConfig *rest.Config
	restMapper meta.RESTMapper
}

func (r *SimpleRESTClientGetter) ToRESTConfig() (*rest.Config, error) {
	return r.restConfig, nil
}

func (r *SimpleRESTClientGetter) ToDiscoveryClient() (discovery.CachedDiscoveryInterface, error) {
	// Always create a fresh discovery client to avoid stale cache issues with CRDs
	discoveryClient, err := discovery.NewDiscoveryClientForConfig(r.restConfig)
	if err != nil {
		return nil, err
	}
	// Wrap in FreshDiscoveryClient to implement CachedDiscoveryInterface
	// This ensures we always get the latest CRDs and API resources
	return &FreshDiscoveryClient{DiscoveryClient: discoveryClient}, nil
}

func (r *SimpleRESTClientGetter) ToRESTMapper() (meta.RESTMapper, error) {
	// Always create a fresh REST mapper to ensure we can discover newly installed CRDs
	discoveryClient, err := r.ToDiscoveryClient()
	if err != nil {
		return nil, err
	}
	return restmapper.NewDeferredDiscoveryRESTMapper(discoveryClient), nil
}

func (r *SimpleRESTClientGetter) ToRawKubeConfigLoader() clientcmd.ClientConfig {
	return nil // Not needed for our use case
}

// ApplierComponents holds all components needed for apply operations
type ApplierComponents struct {
	KubernetesClient       KubernetesClient
	DynamicClient          dynamic.Interface
	DiscoveryClient        discovery.CachedDiscoveryInterface
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
	comps              *ApplierComponents
	liveData           []byte
	spaceID            string
	unitSlug           string
	revisionNum        int64
	waitTimeout        string // WaitTimeout duration string for resource readiness
	inventoryCM        *InventoryConfigMap
	invInfo            inventory.Info
	applyObjects       []*unstructured.Unstructured // Store objects for WaitForApply
	applyCompleted     bool                         // Flag to track if apply has been completed
	destroyObjects     []*unstructured.Unstructured // Store objects for WaitForDestroy
	destroyCompleted   bool                         // Flag to track if destroy has been completed
	liveDataBuilder    *LiveDataBuilder             // Optimized LiveData builder
	lastResourceSet    ResourceSet                  // Store the last ResourceSet for retrieval
	poller             *polling.StatusPoller        // Status poller for kstatus-based waiting
	lastResourceStatus api.ResourceStatusMap        // Store per-resource status from last wait operation
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

	a.ensureConfigHubContextOnObjects(objects)
	a.setDefaultNamespaces(objects)
	log.Log.Info("🚀 Starting apply operation", "count", len(objects))

	// Step 1.5: Check CRD availability before attempting to apply
	// This prevents failures when CRDs are being installed (e.g., by Crossplane)
	if err := a.waitForCRDsAvailable(ctx, objects); err != nil {
		return ApplyResult{Error: fmt.Errorf("CRDs not available: %w", err)}
	}

	// Step 2: Manifest processing - prepare inventory ConfigMap
	// The inventory ConfigMap should be the first document in the manifests
	var inventoryObj *unstructured.Unstructured
	var invInfo inventory.Info

	if a.inventoryCM != nil && a.inventoryCM.IsValid() {
		// Use existing inventory from LiveData
		inventoryObj = a.inventoryCM.Unstructured
		invInfo = a.invInfo
		log.Log.Info("📦 Using existing inventory from LiveData", "id", a.inventoryCM.GetInventoryID())
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

	// Step 4.5: Clear managedFields for SSA compatibility
	// This removes field ownership from other managers (like kubectl), allowing SSA to
	// properly handle array item removal (e.g., removing old initContainers)
	if err := a.clearManagedFieldsForObjects(ctx, resourceObjects); err != nil {
		log.Log.Error(err, "⚠️ Failed to clear managedFields, continuing with apply")
		// Continue anyway - this is best-effort cleanup
	}

	// Run the applier - it returns an event channel
	log.Log.Info("📋 Starting applier with inventory", "namespace", invInfo.GetNamespace(), "id", invInfo.GetID())
	eventChannel := a.comps.Applier.Run(ctx, invInfo, resourceObjects, applyOptions)

	// Drain the event channel with context awareness to handle cancellation
	// This ensures we exit quickly when the operation is overridden/cancelled
	log.Log.Info("📋 Waiting for apply to complete by draining event channel")
	contextCancelled := false
applyDrainLoop:
	for {
		select {
		case <-ctx.Done():
			log.Log.Info("⚠️ Context cancelled while draining apply events, stopping drain",
				"error", ctx.Err(),
				"unitSlug", a.unitSlug)
			contextCancelled = true
			// Drain remaining events in background to avoid goroutine leak
			go func() {
				for range eventChannel {
				}
			}()
			break applyDrainLoop
		case _, ok := <-eventChannel:
			if !ok {
				break applyDrainLoop
			}
			// Event discarded - we use kstatus polling for status tracking
		}
	}
	log.Log.Info("📋 Apply event channel drain completed", "contextCancelled", contextCancelled)

	// Return early if context was cancelled (e.g., operation was overridden)
	if contextCancelled {
		return ApplyResult{
			Error: ErrOperationInterrupted,
		}
	}

	// Now poll for resources to be ready using kstatus
	// Use context deadline if available, otherwise use default timeout
	var waitTimeout time.Duration
	if deadline, ok := ctx.Deadline(); ok {
		remainingTime := time.Until(deadline)
		// Reserve some time for cleanup operations (5 seconds)
		// But ensure we don't exceed the actual deadline
		if remainingTime > 15*time.Second {
			// If we have plenty of time, reserve 5 seconds for cleanup
			waitTimeout = remainingTime - (5 * time.Second)
		} else if remainingTime > 5*time.Second {
			// If time is limited, use most of it but keep at least 1 second for cleanup
			waitTimeout = remainingTime - time.Second
		} else {
			// If we have 5 seconds or less, use whatever time remains
			waitTimeout = remainingTime
		}
		log.Log.Info("⏳ Using context deadline for wait timeout",
			"deadline", deadline.Format(time.RFC3339),
			"remaining", remainingTime.String(),
			"timeout", waitTimeout.String())
	} else {
		waitTimeout = DefaultTimeout // Use the default timeout (60s)
		log.Log.Info("⏳ Using default timeout", "timeout", waitTimeout.String())
	}

	log.Log.Info("⏳ Waiting for resources to be ready")
	if err := a.waitForResourcesReady(ctx, resourceObjects, waitTimeout); err != nil {
		log.Log.Error(err, "Some resources failed to become ready")
		// Continue even if some resources aren't ready - let WaitForApply handle it
	}

	// Store metadata for WaitForApply if needed
	a.applyObjects = resourceObjects
	a.invInfo = invInfo
	a.applyCompleted = true

	// Get live objects to build ResourceSet
	liveObjects, err := a.getLiveObjects(ctx, resourceObjects, true)
	if err != nil {
		log.Log.Error(err, "Failed to get live objects")
		// Return with what we have
		return ApplyResult{
			LiveData:         a.liveData,
			ResourceSet:      NewSimpleResourceSet(),
			ResourceStatuses: a.lastResourceStatus,
			Error:            nil, // Don't fail Apply, let WaitForApply handle it
		}
	}

	// Build ResourceSet from live objects
	simpleResourceSet := NewSimpleResourceSet()
	for _, obj := range liveObjects {
		simpleResourceSet.Add(SimpleResourceSetEntry{
			Name:      obj.GetName(),
			Namespace: obj.GetNamespace(),
			Kind:      obj.GetKind(),
			Action:    "Applied",
		})
	}
	a.lastResourceSet = simpleResourceSet

	// Build LiveData if builder is available
	if a.liveDataBuilder != nil {
		processor := &EventProcessor{} // Empty processor since we discarded events
		updatedLiveData, _, err := a.liveDataBuilder.BuildLiveData(
			ctx,
			invInfo,
			processor,
			a.liveData,
		)
		if err == nil {
			a.liveData = updatedLiveData
			// Note: We keep simpleResourceSet instead of the empty one from BuildLiveData
			// because we discarded events and built the ResourceSet from live objects above
		}
	}

	log.Log.Info("✅ Apply operation completed synchronously",
		"inventoryID", invInfo.GetID(),
		"liveObjectCount", len(liveObjects))

	return ApplyResult{
		LiveData:         a.liveData,
		ResourceSet:      a.lastResourceSet,
		ResourceStatuses: a.lastResourceStatus,
		Error:            nil,
	}
}

// fmtObjMetadata formats object metadata for display
func fmtObjMetadata(obj object.ObjMetadata) string {
	if obj.Namespace != "" {
		return fmt.Sprintf("%s/%s/%s", obj.GroupKind.String(), obj.Namespace, obj.Name)
	}
	return fmt.Sprintf("%s/%s", obj.GroupKind.String(), obj.Name)
}

// waitForResourcesTerminated waits for resources to be deleted from the cluster
func (a *CLIUtilsApplier) waitForResourcesTerminated(ctx context.Context, objects []*unstructured.Unstructured, timeout time.Duration) error {
	if len(objects) == 0 {
		log.Log.Info("No objects to wait for termination")
		return nil
	}

	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	pollInterval := 2 * time.Second

	for _, obj := range objects {
		// Create a polling function for each object
		err := wait.PollUntilContextCancel(waitCtx, pollInterval, true, func(ctx context.Context) (bool, error) {
			key := client.ObjectKey{
				Namespace: obj.GetNamespace(),
				Name:      obj.GetName(),
			}
			u := obj.DeepCopy()
			err := a.comps.KubernetesClient.Get(ctx, key, u)
			if apierrors.IsNotFound(err) {
				// Object is deleted, we're done
				log.Log.V(1).Info("✅ Resource terminated",
					"name", obj.GetName(),
					"namespace", obj.GetNamespace(),
					"kind", obj.GetKind())
				return true, nil
			}
			if err != nil {
				// If context is canceled/timed out, don't keep polling
				// The outer loop will verify actual deletion state
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					log.Log.V(1).Info("⏳ Context done while checking resource",
						"name", obj.GetName(),
						"error", err)
					return false, err
				}
				// Other error, keep polling
				log.Log.V(1).Info("⏳ Error checking resource, will retry",
					"name", obj.GetName(),
					"error", err)
				return false, nil
			}
			// Check if context is cancelled before continuing to poll
			// This ensures faster exit when operation is cancelled/overridden
			select {
			case <-ctx.Done():
				log.Log.V(1).Info("⏳ Context cancelled while waiting for resource termination",
					"name", obj.GetName(),
					"error", ctx.Err())
				return false, ctx.Err()
			default:
			}

			// Object still exists
			log.Log.V(1).Info("⏳ Resource still terminating",
				"name", obj.GetName(),
				"namespace", obj.GetNamespace(),
				"kind", obj.GetKind())
			return false, nil
		})

		if err != nil {
			// Context canceled/timed out - verify actual deletion state with fresh context
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				log.Log.Info("🔍 Verifying resource deletion with fresh context",
					"resource", fmt.Sprintf("%s/%s/%s", obj.GetKind(), obj.GetNamespace(), obj.GetName()))

				verifyCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				key := client.ObjectKey{
					Namespace: obj.GetNamespace(),
					Name:      obj.GetName(),
				}
				u := obj.DeepCopy()
				verifyErr := a.comps.KubernetesClient.Get(verifyCtx, key, u)
				cancel()

				if apierrors.IsNotFound(verifyErr) {
					// Resource is actually deleted - success!
					log.Log.Info("✅ Verified resource is deleted",
						"resource", fmt.Sprintf("%s/%s/%s", obj.GetKind(), obj.GetNamespace(), obj.GetName()))
					continue // Move to next object
				}

				// Resource still exists or we got an error
				if verifyErr != nil {
					log.Log.Error(verifyErr, "Failed to verify resource deletion",
						"resource", fmt.Sprintf("%s/%s/%s", obj.GetKind(), obj.GetNamespace(), obj.GetName()))
				}
				// Fall through to return error
			}

			return fmt.Errorf("timeout waiting for %s/%s/%s termination (unit: %s): %w",
				obj.GetKind(), obj.GetNamespace(), obj.GetName(), a.unitSlug, err)
		}
	}

	log.Log.Info("✅ All resources terminated successfully")
	return nil
}

// waitForResourcesReady waits for resources to be ready using kstatus polling
func (a *CLIUtilsApplier) waitForResourcesReady(ctx context.Context, objects []*unstructured.Unstructured, timeout time.Duration) error {
	// Convert to object metadata for polling
	objectsMeta := object.UnstructuredSetToObjMetadataSet(objects)
	if len(objectsMeta) == 0 {
		log.Log.Info("No objects to wait for")
		return nil
	}
	statusCollector := collector.NewResourceStatusCollector(objectsMeta)

	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Use 2s interval for polling
	pollingOpts := polling.PollOptions{
		PollInterval: 2 * time.Second,
	}

	log.Log.Info("⏳ Starting kstatus polling for resources", "count", len(objects))
	eventsChan := a.poller.Poll(waitCtx, objectsMeta, pollingOpts)

	lastStatus := make(map[object.ObjMetadata]*pollingevent.ResourceStatus)
	failFast := false // Could make this configurable

	done := statusCollector.ListenWithObserver(eventsChan, collector.ObserverFunc(
		func(statusCollector *collector.ResourceStatusCollector, e pollingevent.Event) {
			var rss []*pollingevent.ResourceStatus
			var countFailed int

			for _, rs := range statusCollector.ResourceStatuses {
				if rs == nil {
					continue
				}
				// Skip DeadlineExceeded errors
				if !errors.Is(rs.Error, context.DeadlineExceeded) {
					lastStatus[rs.Identifier] = rs
				}

				if rs.Status == status.FailedStatus {
					countFailed++
				}
				rss = append(rss, rs)
			}

			// Check if we've reached desired state
			desired := status.CurrentStatus
			aggStatus := aggregator.AggregateStatus(rss, desired)
			if aggStatus == desired || (failFast && countFailed > 0) {
				log.Log.Info("✅ Resources reached desired state")
				cancel()
				return
			}
		}),
	)

	<-done

	// Check for errors
	if statusCollector.Error != nil {
		return statusCollector.Error
	}

	// Collect error messages - only for truly failed resources
	var errs []string
	var notReadyResources []string
	var needsVerification []object.ObjMetadata

	for id, rs := range statusCollector.ResourceStatuses {
		switch {
		case rs == nil || lastStatus[id] == nil:
			// Status not captured during polling - need to verify actual cluster state
			// This commonly happens when resources come up very quickly (bulk apply)
			needsVerification = append(needsVerification, id)
		case lastStatus[id].Status == status.FailedStatus:
			// Only FailedStatus is a true error - resources are broken
			var builder strings.Builder
			builder.WriteString(fmt.Sprintf("%s status: '%s'",
				fmtObjMetadata(rs.Identifier), lastStatus[id].Status))
			if rs.Error != nil {
				builder.WriteString(fmt.Sprintf(": %s", rs.Error))
			}
			errs = append(errs, builder.String())
		case lastStatus[id].Status != status.CurrentStatus:
			// Track resources that aren't ready yet (but not failed)
			notReadyResources = append(notReadyResources, fmt.Sprintf("%s (status: %s)",
				fmtObjMetadata(rs.Identifier), lastStatus[id].Status))
		}
	}

	// Verify resources with missing poll status by checking actual cluster state
	if len(needsVerification) > 0 {
		log.Log.Info("🔍 Verifying cluster state for resources with missing poll status",
			"count", len(needsVerification))

		for _, id := range needsVerification {
			// Use a fresh context with short timeout for verification
			verifyCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)

			// Resolve GVK using RESTMapper
			mapping, err := a.comps.RestMapper.RESTMapping(id.GroupKind)
			if err != nil {
				log.Log.Error(err, "Failed to get REST mapping", "resource", fmtObjMetadata(id))
				cancel()
				errs = append(errs, fmt.Sprintf("%s error resolving GVK: %v", fmtObjMetadata(id), err))
				continue
			}

			// Fetch the actual resource from cluster
			u := &unstructured.Unstructured{}
			u.SetGroupVersionKind(mapping.GroupVersionKind)
			u.SetNamespace(id.Namespace)
			u.SetName(id.Name)

			err = a.comps.KubernetesClient.Get(verifyCtx, client.ObjectKey{
				Namespace: id.Namespace,
				Name:      id.Name,
			}, u)

			cancel()

			if err != nil {
				if apierrors.IsNotFound(err) {
					errs = append(errs, fmt.Sprintf("%s not found in cluster", fmtObjMetadata(id)))
				} else {
					errs = append(errs, fmt.Sprintf("%s error fetching: %v", fmtObjMetadata(id), err))
				}
				continue
			}

			// Resource exists - compute its actual status
			result, err := status.Compute(u)
			if err != nil {
				log.Log.Info("⚠️ Could not compute status, assuming progressing",
					"resource", fmtObjMetadata(id), "error", err)
				// Can't compute status but resource exists - treat as progressing
				notReadyResources = append(notReadyResources, fmt.Sprintf("%s (status: unknown, resource exists)",
					fmtObjMetadata(id)))
				continue
			}

			// Check actual status
			switch result.Status {
			case status.CurrentStatus:
				log.Log.Info("✅ Verified resource is ready",
					"resource", fmtObjMetadata(id))
				// Resource is ready - no error
			case status.FailedStatus:
				errs = append(errs, fmt.Sprintf("%s verified status: '%s'", fmtObjMetadata(id), result.Status))
			default:
				// Resource exists but not yet ready - treat as progressing
				notReadyResources = append(notReadyResources, fmt.Sprintf("%s (verified status: %s)",
					fmtObjMetadata(id), result.Status))
			}
		}
	}

	// If there are truly failed resources, return an error
	if len(errs) > 0 {
		msg := fmt.Sprintf("failed early due to stalled resources (unit: %s)", a.unitSlug)
		if errors.Is(waitCtx.Err(), context.DeadlineExceeded) {
			msg = fmt.Sprintf("timeout waiting for resources (unit: %s)", a.unitSlug)
		}
		return fmt.Errorf("%s: [%s]", msg, strings.Join(errs, ", "))
	}

	// If timeout occurred but resources are progressing (not failed), log but don't error
	// This allows resources to continue reconciling in the background
	if errors.Is(waitCtx.Err(), context.DeadlineExceeded) && len(notReadyResources) > 0 {
		log.Log.Info("⏳ Timeout reached but resources are still progressing (not failed)",
			"unit", a.unitSlug,
			"notReady", len(notReadyResources))
		// Don't return an error - resources are progressing normally, just taking longer
		// The apply operation itself succeeded, reconciliation will continue
	}

	// Build per-resource status map for reporting
	a.lastResourceStatus = make(api.ResourceStatusMap)
	now := time.Now()
	for id, rs := range lastStatus {
		if rs == nil {
			continue
		}
		// Build key: "apiVersion/kind#namespace/name" matching K8sResourceProviderType format
		// Use RESTMapper to resolve full GVK from GroupKind.
		// Note: cli-utils ObjMetadata only contains GroupKind, not GroupVersionKind,
		// so we need the RESTMapper to look up the Version component.
		mapping, err := a.comps.RestMapper.RESTMapping(id.GroupKind)
		var resourceType string
		if err != nil {
			log.Log.Error(err, "Failed to resolve GVK", "groupKind", id.GroupKind.String())
			// GVK resolution can fail when CRDs are not installed, the API server is
			// unreachable, or the resource type was deleted between apply and status check.
			// In these cases, we fall back to a "group/kind" key (without version) and mark
			// the resource as ReadinessUnknown since we can't determine its actual state.
			if id.GroupKind.Group != "" {
				resourceType = id.GroupKind.Group + "/" + id.GroupKind.Kind
			} else {
				resourceType = id.GroupKind.Kind
			}
			resourceName := id.Namespace + "/" + id.Name
			key := funcApi.ResourceTypeAndName(resourceType + "#" + resourceName)
			a.lastResourceStatus[key] = api.ResourceStatus{
				SyncStatus: api.ResourceSyncStatusSynced,
				Readiness:  api.ResourceReadinessUnknown,
				Message:    fmt.Sprintf("Failed to resolve GVK: %v", err),
				UpdatedAt:  now,
			}
			continue
		}
		gvk := mapping.GroupVersionKind
		if gvk.Group != "" {
			resourceType = gvk.Group + "/" + gvk.Version + "/" + gvk.Kind
		} else {
			resourceType = gvk.Version + "/" + gvk.Kind
		}
		// Always use namespace/name format, even for cluster-scoped resources (empty namespace)
		resourceName := id.Namespace + "/" + id.Name
		key := funcApi.ResourceTypeAndName(resourceType + "#" + resourceName)

		resourceStatus := api.ResourceStatus{
			SyncStatus: api.ResourceSyncStatusSynced, // If we're in wait phase, apply already succeeded
			UpdatedAt:  now,
		}

		// Map kstatus to our readiness type
		switch rs.Status {
		case status.CurrentStatus:
			resourceStatus.Readiness = api.ResourceReadinessReady
		case status.InProgressStatus:
			resourceStatus.Readiness = api.ResourceReadinessInProgress
		case status.FailedStatus:
			resourceStatus.Readiness = api.ResourceReadinessFailed
			if rs.Error != nil {
				resourceStatus.Message = rs.Error.Error()
			}
		case status.TerminatingStatus:
			resourceStatus.Readiness = api.ResourceReadinessTerminating
		default:
			resourceStatus.Readiness = api.ResourceReadinessUnknown
		}

		a.lastResourceStatus[key] = resourceStatus
	}

	log.Log.Info("📊 Built per-resource status map", "count", len(a.lastResourceStatus))

	return nil
}

// WaitForApply implements K8sApplier.WaitForApply
func (a *CLIUtilsApplier) WaitForApply(ctx context.Context, objects []*unstructured.Unstructured, timeout time.Duration) WaitResult {
	if err := a.validate(); err != nil {
		return WaitResult{Error: err}
	}

	// Ensure we have objects to work with
	if len(objects) == 0 && len(a.applyObjects) > 0 {
		objects = a.applyObjects
	}
	a.setDefaultNamespaces(objects)

	// Check if Apply has already completed
	if !a.applyCompleted {
		// Give Apply operation a moment to complete if it's running
		log.Log.Info("⏳ Waiting for Apply to complete")
		time.Sleep(2 * time.Second)
	}

	// Use the reusable wait function
	if err := a.waitForResourcesReady(ctx, objects, timeout); err != nil {
		return WaitResult{Error: err}
	}

	// Get live objects from cluster
	// Use a fresh context with timeout since the parent context may have expired during wait
	// Use the WaitTimeout value if set, otherwise default to 30 seconds
	getLiveTimeout := 30 * time.Second
	if timeout > 0 {
		getLiveTimeout = timeout
	}
	getLiveCtx, cancel := context.WithTimeout(context.Background(), getLiveTimeout)
	defer cancel()

	// Return uncleaned live objects - caller will cleanup for LiveData, keep uncleaned for LiveState
	liveObjects, err := a.getLiveObjects(getLiveCtx, objects, false)
	if err != nil {
		log.Log.Error(err, "Failed to get live objects after successful wait")
		// Try to get whatever we can using the fresh context
		liveObjects = make([]*unstructured.Unstructured, 0)
		for _, obj := range objects {
			key := client.ObjectKey{
				Namespace: obj.GetNamespace(),
				Name:      obj.GetName(),
			}
			u := obj.DeepCopyObject().(*unstructured.Unstructured)
			if getErr := a.comps.KubernetesClient.Get(getLiveCtx, key, u); getErr == nil {
				// Don't cleanup - caller will cleanup for LiveData, keep uncleaned for LiveState
				liveObjects = append(liveObjects, u)
			}
		}
	}

	// Build ResourceSet
	simpleResourceSet := NewSimpleResourceSet()
	for _, obj := range liveObjects {
		simpleResourceSet.Add(SimpleResourceSetEntry{
			Name:      obj.GetName(),
			Namespace: obj.GetNamespace(),
			Kind:      obj.GetKind(),
			Action:    "Applied",
		})
	}
	a.lastResourceSet = simpleResourceSet
	a.applyCompleted = true

	log.Log.Info("✅ WaitForApply completed",
		"liveObjectCount", len(liveObjects),
		"changeSetEntries", len(a.lastResourceSet.GetEntries()))

	return WaitResult{
		LiveObjects:      liveObjects,
		ResourceSet:      a.lastResourceSet,
		ResourceStatuses: a.lastResourceStatus,
	}
}

// Refresh implements K8sApplier.Refresh
// Returns uncleaned live objects - caller will cleanup for LiveData, keep uncleaned for LiveState
func (a *CLIUtilsApplier) Refresh(ctx context.Context, objects []*unstructured.Unstructured) ([]*unstructured.Unstructured, error) {
	if err := a.validate(); err != nil {
		return nil, err
	}

	a.setDefaultNamespaces(objects)
	// TODO: This should not return an error in the case of Not Found
	// Return uncleaned live objects - caller will cleanup for LiveData, keep uncleaned for LiveState
	return a.getLiveObjects(ctx, objects, false)
}

// Destroy implements K8sApplier.Destroy following the CLI-Utils algorithm
func (a *CLIUtilsApplier) Destroy(ctx context.Context, objects []*unstructured.Unstructured) DestroyResult {
	log.Log.Info("🔍 Destroy called",
		"unitSlug", a.unitSlug,
		"spaceID", a.spaceID,
		"objectCount", len(objects),
		"hasInventoryCM", a.inventoryCM != nil,
		"hasInvInfo", a.invInfo != nil,
		"destroyCompleted", a.destroyCompleted)

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
		// Use existing inventory from LiveData
		invInfo = a.invInfo
		log.Log.Info("📦 Using existing inventory from LiveData for destroy",
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

	// Step 2.5: Ensure inventory is populated for backward compatibility
	// For old units without inventory tracking, populate inventory from input objects
	if len(objects) > 0 {
		existingInv, err := a.comps.InventoryClient.Get(ctx, invInfo, inventory.GetOptions{})
		inventoryIsEmpty := err != nil || existingInv == nil || len(existingInv.GetObjectRefs()) == 0

		if inventoryIsEmpty {
			log.Log.Info("📦 Inventory is empty, populating from input objects for backward compatibility",
				"objectCount", len(objects),
				"unitSlug", a.unitSlug)

			if invClient, ok := a.comps.InventoryClient.(*InMemInventoryClient); ok {
				if populateErr := invClient.PopulateFromObjects(ctx, invInfo, objects); populateErr != nil {
					log.Log.Error(populateErr, "⚠️ Failed to populate inventory from objects, destroy may fail",
						"unitSlug", a.unitSlug)
				} else {
					log.Log.Info("✅ Inventory populated from input objects",
						"count", len(objects),
						"unitSlug", a.unitSlug)
				}
			}
		}
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
	log.Log.Info("📋 Starting destroyer with inventory",
		"namespace", invInfo.GetNamespace(),
		"id", invInfo.GetID(),
		"unitSlug", a.unitSlug)

	eventChannel := a.comps.Destroyer.Run(ctx, invInfo, destroyOptions)

	// Drain the event channel with context awareness to handle cancellation
	// This ensures we exit quickly when the operation is overridden/cancelled
	log.Log.Info("📋 Waiting for destroy to complete by draining event channel")
	contextCancelled := false
drainLoop:
	for {
		select {
		case <-ctx.Done():
			log.Log.Info("⚠️ Context cancelled while draining destroy events, stopping drain",
				"error", ctx.Err(),
				"unitSlug", a.unitSlug)
			contextCancelled = true
			// Drain remaining events in background to avoid goroutine leak
			go func() {
				for range eventChannel {
				}
			}()
			break drainLoop
		case _, ok := <-eventChannel:
			if !ok {
				break drainLoop
			}
			// Event discarded - we use polling for status tracking
		}
	}
	log.Log.Info("📋 Destroy event channel drain completed", "contextCancelled", contextCancelled)

	// Return early if context was cancelled (e.g., operation was overridden)
	if contextCancelled {
		return DestroyResult{
			Error: ErrOperationInterrupted,
		}
	}

	// Now poll for resources to be terminated using polling
	// Get list of objects to wait for termination
	var objectsToDelete []*unstructured.Unstructured
	if len(objects) > 0 {
		objectsToDelete = objects
	} else {
		// If no objects provided, we should get them from inventory
		// This would require reading the inventory from cluster
		// For now, we'll just use what was provided
		objectsToDelete = a.destroyObjects
	}

	// Use context deadline if available, otherwise use default timeout
	var waitTimeout time.Duration
	if deadline, ok := ctx.Deadline(); ok {
		remainingTime := time.Until(deadline)
		// Reserve some time for cleanup operations (5 seconds)
		// But ensure we don't exceed the actual deadline
		if remainingTime > 15*time.Second {
			// If we have plenty of time, reserve 5 seconds for cleanup
			waitTimeout = remainingTime - (5 * time.Second)
		} else if remainingTime > 5*time.Second {
			// If time is limited, use most of it but keep at least 1 second for cleanup
			waitTimeout = remainingTime - time.Second
		} else {
			// If we have 5 seconds or less, use whatever time remains
			waitTimeout = remainingTime
		}
		log.Log.Info("⏳ Using context deadline for wait timeout",
			"deadline", deadline.Format(time.RFC3339),
			"remaining", remainingTime.String(),
			"timeout", waitTimeout.String())
	} else {
		waitTimeout = DefaultTimeout // Use the default timeout (60s)
		log.Log.Info("⏳ Using default timeout", "timeout", waitTimeout.String())
	}

	log.Log.Info("⏳ Waiting for resources to be terminated")
	if err := a.waitForResourcesTerminated(ctx, objectsToDelete, waitTimeout); err != nil {
		log.Log.Error(err, "Some resources failed to terminate")
		// Continue even if some resources aren't terminated - let WaitForDestroy handle it
	}

	// Store metadata for WaitForDestroy if needed
	a.destroyObjects = objectsToDelete
	a.invInfo = invInfo
	a.destroyCompleted = true

	// Build ResourceSet for destroyed resources
	simpleResourceSet := NewSimpleResourceSet()
	for _, obj := range objectsToDelete {
		simpleResourceSet.Add(SimpleResourceSetEntry{
			Name:      obj.GetName(),
			Namespace: obj.GetNamespace(),
			Kind:      obj.GetKind(),
			Action:    "Deleted",
		})
	}
	a.lastResourceSet = simpleResourceSet

	log.Log.Info("✅ Destroy operation completed synchronously",
		"inventoryID", invInfo.GetID(),
		"unitSlug", a.unitSlug,
		"deletedCount", len(objectsToDelete))

	return DestroyResult{
		LiveData:    nil, // No live state after destroy
		ResourceSet: a.lastResourceSet,
		Error:       nil,
	}
}

// WaitForDestroy implements K8sApplier.WaitForDestroy
func (a *CLIUtilsApplier) WaitForDestroy(ctx context.Context, objects []*unstructured.Unstructured, timeout time.Duration) WaitResult {
	if err := a.validate(); err != nil {
		return WaitResult{Error: err}
	}

	// Ensure we have objects to work with
	if len(objects) == 0 && len(a.destroyObjects) > 0 {
		objects = a.destroyObjects
	}
	a.setDefaultNamespaces(objects)

	// Check if destroy has already been completed
	if a.destroyCompleted {
		log.Log.Info("✅ Destroy already completed", "unitSlug", a.unitSlug)
		return WaitResult{
			LiveObjects: []*unstructured.Unstructured{},
			ResourceSet: a.lastResourceSet,
			Error:       nil,
		}
	}

	// Check if Destroy hasn't been called yet
	if !a.destroyCompleted {
		// Give Destroy operation a moment to complete if it's running
		log.Log.Info("⏳ Waiting for Destroy to complete")
		time.Sleep(2 * time.Second)
	}

	// Use the reusable wait function for termination
	if err := a.waitForResourcesTerminated(ctx, objects, timeout); err != nil {
		return WaitResult{Error: err}
	}

	// Build ResourceSet for destroyed resources
	simpleResourceSet := NewSimpleResourceSet()
	for _, obj := range objects {
		simpleResourceSet.Add(SimpleResourceSetEntry{
			Name:      obj.GetName(),
			Namespace: obj.GetNamespace(),
			Kind:      obj.GetKind(),
			Action:    "Deleted",
		})
	}
	a.lastResourceSet = simpleResourceSet
	a.destroyCompleted = true

	log.Log.Info("✅ WaitForDestroy completed",
		"deletedCount", len(objects),
		"changeSetEntries", len(a.lastResourceSet.GetEntries()))

	return WaitResult{
		LiveObjects: []*unstructured.Unstructured{}, // No live objects after destroy
		ResourceSet: a.lastResourceSet,
		Error:       nil,
	}
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
	// 1. Corrupted LiveData: If LiveData contains a malformed inventory ConfigMap with missing metadata fields
	// 2. Parsing Errors: When SplitInventoryFromLiveData encounters a ConfigMap without proper inventory labels/annotations
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

func (a *CLIUtilsApplier) ensureConfigHubContextOnObjects(objects []*unstructured.Unstructured) {
	for _, obj := range objects {
		ensureConfigHubContext(obj, a.unitSlug, a.spaceID, a.revisionNum)
	}
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
	// Initialize klog for CLI-Utils debugging (only once)
	initKlog()

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
	// Wrap discovery client to always return fresh data (no caching)
	// This ensures we can discover newly installed CRDs
	freshDiscovery := &FreshDiscoveryClient{DiscoveryClient: discoveryClient}
	restMapper := restmapper.NewDeferredDiscoveryRESTMapper(freshDiscovery)

	// Create kubectl factory using SimpleRESTClientGetter to avoid cached disk discovery
	// This prevents errors when running in read-only filesystems (e.g., Kubernetes pods)
	// where the default ConfigFlags would try to write to ~/.kube/cache/discovery/
	// The alternative would be to set the cache directory to be under /tmp and add
	// a /tmp emptyDir volume to the worker, which needs to be done for other reasons.
	// The reason we didn't do that here is for consistency across different Kubernetes
	// client calls.
	restClientGetter := &SimpleRESTClientGetter{
		restConfig: cfg,
		restMapper: restMapper,
	}
	factory := util.NewFactory(restClientGetter)

	// Determine inventory client based on LiveData
	var invClient inventory.Client
	var invInfo inventory.Info
	var inventoryCM *InventoryConfigMap

	if len(config.LiveData) > 0 {
		log.Log.Info("📦 Using in-memory inventory from LiveData")
		invClient, inventoryCM, _, err = CreateInventoryFromLiveData(context.Background(), config.LiveData, defaultInvInfo)
		if err != nil {
			log.Log.Error(err, "⚠️ Failed to create inventory from LiveData, falling back to default")
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

	log.Log.Info("✅ Created applier components", "hasLiveData", len(config.LiveData) > 0)

	return &ApplierComponents{
		KubernetesClient: k8sClient,
		DynamicClient:    dynamicClient,
		DiscoveryClient:  freshDiscovery,
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

	// Create the optimized LiveDataBuilder
	liveDataBuilder := NewLiveDataBuilder(
		comps.InventoryClient,
		comps.DynamicClient,
		comps.RestMapper,
		config.SpaceID,
		config.UnitSlug,
	)

	// Create StatusPoller for kstatus-based waiting
	poller := polling.NewStatusPoller(comps.KubernetesClient, comps.RestMapper, polling.Options{})

	log.Log.Info("🚀 Created CLIUtilsApplier with LiveDataBuilder and StatusPoller")

	return &CLIUtilsApplier{
		comps:           comps,
		liveData:        config.LiveData,
		spaceID:         config.SpaceID,
		unitSlug:        config.UnitSlug,
		revisionNum:     config.RevisionNum,
		waitTimeout:     config.WaitTimeout,
		inventoryCM:     inventoryCM,
		invInfo:         invInfo,
		liveDataBuilder: liveDataBuilder,
		poller:          poller,
	}, nil
}

// waitForCRDsAvailable checks and waits for all CRDs required by the objects to be available
func (a *CLIUtilsApplier) waitForCRDsAvailable(ctx context.Context, objects []*unstructured.Unstructured) error {
	// Extract unique GVKs from objects
	gvkSet := make(map[string]schema.GroupVersionKind)
	for _, obj := range objects {
		gvk := obj.GroupVersionKind()
		// Skip core resources (no group) as they don't need CRDs
		if gvk.Group != "" {
			key := fmt.Sprintf("%s/%s/%s", gvk.Group, gvk.Version, gvk.Kind)
			gvkSet[key] = gvk
		}
	}

	if len(gvkSet) == 0 {
		log.Log.Info("No CRDs to check - all resources are core types")
		return nil
	}

	log.Log.Info("Checking CRD availability", "count", len(gvkSet))

	// Use the fresh discovery client we already have to avoid cache issues
	discoveryClient := a.comps.DiscoveryClient

	// Calculate timeout: use the WaitTimeout for CRD checks (cancellable via parent context)
	// Default to LargeWaitTimeout if not specified
	var crdCheckTimeout time.Duration = LargeWaitTimeout
	if a.waitTimeout != "" {
		if waitDuration, err := time.ParseDuration(a.waitTimeout); err == nil {
			crdCheckTimeout = waitDuration
			log.Log.Info("Using WaitTimeout for CRD checks", "timeout", crdCheckTimeout)
		} else {
			log.Log.Info("Invalid WaitTimeout, using LargeWaitTimeout for CRD checks", "timeout", a.waitTimeout, "error", err)
		}
	}

	// Check with retries using exponential backoff
	checkCtx, cancel := context.WithTimeout(ctx, crdCheckTimeout)
	defer cancel()

	retryCount := 0
	for {
		select {
		case <-checkCtx.Done():
			return fmt.Errorf("timeout waiting for CRDs to be available")
		default:
			// Check each GVK
			missingCRDs := []string{}

			// Force fresh discovery to check current state
			if freshDiscovery, ok := discoveryClient.(*FreshDiscoveryClient); ok {
				freshDiscovery.Invalidate()
			}
			_, apiResourceList, err := discoveryClient.ServerGroupsAndResources()
			if err != nil {
				// Partial discovery errors are common, continue checking
				log.Log.V(2).Info("Discovery error (continuing)", "error", err)
			}

			for key, gvk := range gvkSet {
				found := false
				for _, apiResource := range apiResourceList {
					gv, err := schema.ParseGroupVersion(apiResource.GroupVersion)
					if err != nil {
						continue
					}
					if gv.Group == gvk.Group && gv.Version == gvk.Version {
						for _, resource := range apiResource.APIResources {
							if resource.Kind == gvk.Kind {
								found = true
								break
							}
						}
					}
					if found {
						break
					}
				}

				if !found {
					missingCRDs = append(missingCRDs, key)
				}
			}

			if len(missingCRDs) == 0 {
				log.Log.Info("✅ All required CRDs are available")
				return nil
			}

			// Calculate backoff delay
			var retryDelay time.Duration
			if retryCount == 0 {
				retryDelay = 2 * time.Second
			} else if retryCount < 5 {
				retryDelay = time.Duration(1<<uint(retryCount)) * time.Second // exponential: 2s, 4s, 8s, 16s, 32s
			} else {
				retryDelay = 30 * time.Second // cap at 30s
			}

			log.Log.Info("⏳ Waiting for CRDs to be available",
				"missing", missingCRDs,
				"retry", retryCount+1,
				"nextCheck", retryDelay)

			select {
			case <-time.After(retryDelay):
				retryCount++
			case <-checkCtx.Done():
				return fmt.Errorf("timeout waiting for CRDs: %v", missingCRDs)
			}
		}
	}
}

var otherApplierManagers = map[string]bool{
	// Note(Brian): I have not seen before-first-apply in current versions.
	// kubectl create uses the manager "kubectl-create". Default fields don't
	// appear in managedFields.
	// https://github.com/kubernetes/kubernetes/issues/89954
	// https://github.com/kubernetes/kubernetes/issues/131476
	"before-first-apply": true, // Legacy default field manager

	"helm":                 true, // Helm
	"helm-controller":      true, // Flux HelmRelease
	"kustomize-controller": true, // Flux Kustomization
	"argocd-controller":    true, // ArgoCD's default: ArgoCDSSAManager
	"tanka":                true, // Tanka
}

// shouldTakeOverManager checks if a field manager should be replaced by our manager.
// We take over kubectl-* managers and old confighub managers to enable proper SSA field deletion.
// We preserve managers from other controllers (HPA, VPA, etc.) to avoid conflicts.
// https://kubernetes.io/docs/reference/using-api/server-side-apply/#transferring-ownership
func shouldTakeOverManager(manager string) bool {
	if manager == FieldManager {
		return false
	}
	// Take over kubectl managers (kubectl-client-side-apply, kubectl-edit, etc.)
	if strings.HasPrefix(manager, "kubectl") {
		return true
	}
	// Take over old confighub managers to consolidate ownership
	if strings.HasPrefix(manager, "confighub") {
		return true
	}
	// Take over other whole-resource appliers the user may be transitioning from
	if otherApplierManagers[manager] {
		return true
	}
	return false
}

// clearManagedFieldsForObjects removes kubectl and old confighub managers from resources before apply.
// This allows SSA to properly handle array item removal (e.g., removing old initContainers)
// while preserving field ownership from other controllers (HPA, VPA, etc.).
// This follows the pattern used by Flux kustomize-controller (PR #527).
// TODO: See https://github.com/kubernetes/kubernetes/issues/99003
// We may want to change the manager so that we can remove fields not specified in ConfigHub.
func (a *CLIUtilsApplier) clearManagedFieldsForObjects(ctx context.Context, objects []*unstructured.Unstructured) error {
	for _, obj := range objects {
		key := client.ObjectKey{
			Namespace: obj.GetNamespace(),
			Name:      obj.GetName(),
		}

		// Get the current resource from cluster
		current := obj.DeepCopy()
		err := a.comps.KubernetesClient.Get(ctx, key, current)
		if err != nil {
			if apierrors.IsNotFound(err) {
				// Resource doesn't exist yet, no need to clear managedFields
				continue
			}
			return fmt.Errorf("failed to get resource %s/%s: %w", obj.GetNamespace(), obj.GetName(), err)
		}

		// Check if managedFields has entries that we should take over
		managedFields := current.GetManagedFields()
		if len(managedFields) == 0 {
			continue
		}

		// Filter managedFields: keep entries from other controllers, remove kubectl/confighub
		var filteredFields []metav1.ManagedFieldsEntry
		var removedManagers []string
		for _, mf := range managedFields {
			if shouldTakeOverManager(mf.Manager) {
				removedManagers = append(removedManagers, mf.Manager)
			} else {
				filteredFields = append(filteredFields, mf)
			}
		}

		// If no managers need to be removed, skip this object
		if len(removedManagers) == 0 {
			continue
		}

		log.Log.Info("🔧 Removing kubectl/confighub managers for SSA compatibility",
			"name", obj.GetName(),
			"namespace", obj.GetNamespace(),
			"kind", obj.GetKind(),
			"removedManagers", removedManagers,
			"preservedCount", len(filteredFields))

		// Use JSON Patch to only update managedFields without touching the spec
		// This avoids validation errors from sending the full resource back
		var patch []byte
		if len(filteredFields) == 0 {
			// Remove all managedFields
			patch = []byte(`[{"op":"remove","path":"/metadata/managedFields"}]`)
		} else {
			// Replace with filtered managedFields - need to serialize the entries
			managedFieldsJSON, err := json.Marshal(filteredFields)
			if err != nil {
				log.Log.Error(err, "⚠️ Failed to marshal managedFields, continuing anyway",
					"name", obj.GetName(),
					"namespace", obj.GetNamespace())
				continue
			}
			patch = []byte(fmt.Sprintf(`[{"op":"replace","path":"/metadata/managedFields","value":%s}]`, managedFieldsJSON))
		}

		if err := a.comps.KubernetesClient.Patch(ctx, current, client.RawPatch(types.JSONPatchType, patch)); err != nil {
			log.Log.Error(err, "⚠️ Failed to patch managedFields, continuing anyway",
				"name", obj.GetName(),
				"namespace", obj.GetNamespace())
			// Continue anyway - this is best-effort
		}
	}
	return nil
}
