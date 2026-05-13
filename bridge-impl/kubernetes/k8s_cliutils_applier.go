// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package kubernetes

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/confighub/sdk/configkit/k8skit"
	"github.com/confighub/sdk/core/configkit/yamlkit"
	funcApi "github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/worker/api"
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
	"sigs.k8s.io/cli-utils/pkg/object"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/confighub/sdk/bridge-impl/kubernetes/statuspoller"
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
	MinimalTimeout       = 10 * time.Millisecond
	PollInterval         = 2 * time.Second
	InventoryPrefix      = "inventory"
)

// Wait-loop cadence. The three knobs must satisfy the ordering invariant
// asserted in TestCadenceInvariants:
//
//	kstatusPollInterval <= tickInterval     so classifiers see fresh kstatus state.
//	tickInterval        <  stuckThreshold   so time-based classifiers fire before the grace period ends.
//	stuckThreshold      <  progressingTimeout (default = 5*stuckThreshold in statuspoller).
const (
	kstatusPollInterval = 2 * time.Second
	tickInterval        = 5 * time.Second
	stuckThreshold      = 30 * time.Second
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

// cliUtilsApplier is the narrow subset of *apply.Applier that
// runApplyAndDrain depends on. Defining it as an interface here lets
// the orchestration be unit-tested with a fake that streams a scripted
// sequence of events, without dragging in cli-utils' real Applier
// (which needs a live REST config and inventory client).
type cliUtilsApplier interface {
	Run(ctx context.Context, inv inventory.Info, objects object.UnstructuredSet, opts apply.ApplierOptions) <-chan event.Event
}

// cliUtilsDestroyer is the narrow subset of *apply.Destroyer that
// runDestroyAndDrain depends on, parallel to cliUtilsApplier.
type cliUtilsDestroyer interface {
	Run(ctx context.Context, inv inventory.Info, opts apply.DestroyerOptions) <-chan event.Event
}

// ApplierComponents holds all components needed for apply operations
type ApplierComponents struct {
	KubernetesClient       KubernetesClient
	DynamicClient          dynamic.Interface
	DiscoveryClient        discovery.CachedDiscoveryInterface
	RestConfig             *rest.Config
	RestMapper             meta.RESTMapper
	Applier                cliUtilsApplier
	Destroyer              cliUtilsDestroyer
	InventoryClient        inventory.Client
	InventoryInfo          inventory.Info
	ServerSideOptions      common.ServerSideOptions
	ReconcileTimeout       time.Duration
	PruneTimeout           time.Duration
	PrunePropagationPolicy metav1.DeletionPropagation
	InventoryPolicy        inventory.Policy
}

// CLIUtilsApplier implements K8sApplier using kubernetes-sigs/cli-utils.
//
// Lifecycle: a fresh CLIUtilsApplier is constructed per bridge handler
// invocation (KubernetesBridgeWorker.GetOrCreateApplier returns a new
// instance for every Apply / WaitForApply / Destroy / WaitForDestroy /
// Refresh call). The applier therefore holds no state that crosses calls;
// no protocol exists between Apply and WaitForApply on the same instance.
// Each method does its own work end to end and returns the result through
// ApplyResult / WaitResult / DestroyResult.
//
// All fields are populated at construction and never mutated afterwards,
// except liveData: Apply may rewrite it after the LiveDataBuilder produces
// an updated serialization, and the rewritten bytes are returned in the
// same ApplyResult.
//
// Per-call computed values (ResourceSet, ResourceStatusMap) are kept on the
// stack and threaded through return values rather than stashed on the
// receiver. The wctx is also per-call: WaitForApply receives it as a
// parameter and uses wctx.SendStatus directly, so no stale per-instance
// emitter can outlive its request.
type CLIUtilsApplier struct {
	comps              *ApplierComponents
	liveData           []byte
	spaceID            string
	unitSlug           string
	revisionNum        int64
	waitTimeout        string // WaitTimeout duration string for resource readiness
	enforcedNamespace  string // If non-empty, every namespaced resource's metadata.namespace must equal this value
	inventoryCM        *InventoryConfigMap
	invInfo            inventory.Info
	liveDataBuilder    *LiveDataBuilder      // Optimized LiveData builder
	poller             *polling.StatusPoller // Status poller for kstatus-based waiting
	dryRun             bool                  // When true, use server-side dry run
	progressingTimeout time.Duration         // Time-based Stuck fallback; 0 = statuspoller default
}

// dryRunStrategy returns the appropriate DryRunStrategy based on the dryRun flag.
func (a *CLIUtilsApplier) dryRunStrategy() common.DryRunStrategy {
	if a.dryRun {
		return common.DryRunServer
	}
	return common.DryRunNone
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

// drainResult is the outcome of consuming a cli-utils event channel to
// completion (or until ctx is cancelled).
//
//   - contextCancelled is true when the caller's context fired before the
//     event channel closed. Callers must treat this as a terminal interrupt
//     and return ErrOperationInterrupted; the channel is being drained in a
//     background goroutine so the cli-utils sender can still complete.
//   - failures collects per-resource error messages selected by the
//     caller's failureFor predicate. Callers join them into a single
//     ApplyResult/DestroyResult Error.
type drainResult struct {
	contextCancelled bool
	failures         []string
}

// drainEventChannel consumes cli-utils events from ch until the channel
// closes or ctx is cancelled. failureFor classifies each event: a
// non-empty return is appended to drainResult.failures; "" means the
// event is uninteresting for terminal-error reporting (it may still be
// logged inside the predicate).
//
// On ctx cancellation, drainEventChannel forks a goroutine to keep
// reading the channel so the cli-utils sender — which blocks on send —
// can finish writing and shut down cleanly. Without this, cancellation
// would leak the sender goroutine.
func drainEventChannel(
	ctx context.Context,
	ch <-chan event.Event,
	failureFor func(event.Event) string,
) drainResult {
	var r drainResult
	for {
		select {
		case <-ctx.Done():
			r.contextCancelled = true
			go func() {
				for range ch {
				}
			}()
			return r
		case e, ok := <-ch:
			if !ok {
				return r
			}
			if msg := failureFor(e); msg != "" {
				r.failures = append(r.failures, msg)
			}
		}
	}
}

// applyEventFailure is the failureFor predicate for Apply's event drain.
// It returns the per-resource error message to accumulate; "" for events
// that are observed (and possibly logged) but not terminal failures.
//
// ApplyFailed and ApplySkipped are both terminal: cli-utils emits
// ApplySkipped when an apply filter rejects the resource (inventory
// policy mismatch, unmet dependency, missing local namespace, …). In
// every case the resource was not actuated, so the bridge's apply
// intent was not fulfilled and the caller must see a non-empty error
// rather than a silent partial success. ApplySuccessful contributes
// nothing.
func applyEventFailure(e event.Event) string {
	switch e.Type {
	case event.ErrorType:
		log.Log.Error(e.ErrorEvent.Err, "❌ Apply error event")
		return e.ErrorEvent.Err.Error()
	case event.ApplyType:
		switch e.ApplyEvent.Status {
		case event.ApplyFailed:
			if e.ApplyEvent.Error != nil {
				log.Log.Error(e.ApplyEvent.Error, "❌ Failed to apply resource",
					"identifier", e.ApplyEvent.Identifier.String())
				return fmt.Sprintf("%s: %s", e.ApplyEvent.Identifier, e.ApplyEvent.Error)
			}
		case event.ApplySkipped:
			// Always carries a non-nil filter reason from cli-utils
			// (apply_task.go createApplySkippedEvent). Defensive on
			// nil to keep the drain robust if the upstream contract
			// changes.
			reason := "skipped by apply filter"
			if e.ApplyEvent.Error != nil {
				reason = e.ApplyEvent.Error.Error()
			}
			log.Log.Error(e.ApplyEvent.Error, "⚠️ Resource not applied",
				"identifier", e.ApplyEvent.Identifier.String(),
				"reason", reason)
			return fmt.Sprintf("%s: %s", e.ApplyEvent.Identifier, reason)
		}
	case event.PruneType:
		// Prune failures are logged but not treated as terminal: the
		// resource didn't apply cleanly, but it's an orphan-removal
		// concern, not a failed deployment of intent.
		if e.PruneEvent.Status == event.PruneFailed && e.PruneEvent.Error != nil {
			log.Log.Error(e.PruneEvent.Error, "❌ Failed to prune resource",
				"identifier", e.PruneEvent.Identifier.String())
		}
	}
	return ""
}

// destroyEventFailure is the failureFor predicate for Destroy's event drain.
func destroyEventFailure(e event.Event) string {
	switch e.Type {
	case event.ErrorType:
		log.Log.Error(e.ErrorEvent.Err, "❌ Destroy error event")
		return e.ErrorEvent.Err.Error()
	case event.DeleteType:
		if e.DeleteEvent.Status == event.DeleteFailed && e.DeleteEvent.Error != nil {
			log.Log.Error(e.DeleteEvent.Error, "❌ Failed to delete resource",
				"identifier", e.DeleteEvent.Identifier.String())
			return fmt.Sprintf("%s: %s", e.DeleteEvent.Identifier, e.DeleteEvent.Error)
		}
	case event.ApplyType:
		// Apply failures during destroy are logged for diagnostics but
		// not terminal: destroy can complete even if some apply-shaped
		// inventory updates fail.
		if e.ApplyEvent.Status == event.ApplyFailed && e.ApplyEvent.Error != nil {
			log.Log.Error(e.ApplyEvent.Error, "❌ Failed to apply resource during destroy",
				"identifier", e.ApplyEvent.Identifier.String())
		}
	}
	return ""
}

// Apply implements K8sApplier.Apply following the CLI-Utils algorithm
func (a *CLIUtilsApplier) Apply(wctx api.BridgeWorkerContext, objects []*unstructured.Unstructured) ApplyResult {
	ctx := wctx.Context()
	// Step 1: Input validation
	if err := a.validate(); err != nil {
		return ApplyResult{Error: err}
	}

	a.ensureConfigHubContextOnObjects(objects)
	if err := a.validateNamespaces(objects); err != nil {
		return ApplyResult{Error: err}
	}
	log.Log.Info("🚀 Starting apply operation", "count", len(objects))

	// Step 1.5: Check CRD availability before attempting to apply
	// This prevents failures when CRDs are being installed (e.g., by Crossplane)
	if err := a.waitForCRDsAvailable(ctx, objects); err != nil {
		return ApplyResult{Error: fmt.Errorf("CRDs not available: %w", err)}
	}

	// Step 2: Resolve inventory info — prefer the inventory loaded at
	// construction; otherwise fabricate one from the first object's
	// annotations (the brand-new-unit path).
	invInfo, err := a.resolveInventoryInfo(objects)
	if err != nil {
		return ApplyResult{Error: err}
	}

	// Step 2.5: Pre-flight inventory partition. Splits objects into
	// "to apply" and "conflict" buckets; cross-bridge shared resources
	// already in place under another inventory are dropped from the
	// apply set entirely so cli-utils does not skip them mid-apply.
	// Surfacing the conflict here gives the operator a single error
	// naming the owning unit and avoids partial actuation.
	toApply, conflicts, err := a.partitionByInventoryOwnership(ctx, invInfo.GetID().String(), objects)
	if err != nil {
		return ApplyResult{Error: err}
	}
	if conflictErr := formatInventoryConflicts(conflicts); conflictErr != nil {
		log.Log.Error(conflictErr, "❌ Cross-inventory conflict detected", "count", len(conflicts))
		return ApplyResult{Error: conflictErr}
	}

	// Step 3: Apply Execution
	//
	// Apply is the "submit" half of ConfigHub's two-phase model: push config
	// to the target and return. Waiting for readiness is the job of
	// WaitForApply, which reports progress (including Stuck) back to the
	// server as it observes kstatus changes.
	if err := a.runApplyAndDrain(ctx, invInfo, toApply); err != nil {
		return ApplyResult{Error: err}
	}

	// In dry run mode, skip waiting for resources and fetching live objects.
	// Server-side dry run validates without persisting, so there's nothing to
	// poll, and no per-resource readiness exists yet — return an empty
	// ResourceSet and nil ResourceStatuses.
	if a.dryRun {
		log.Log.Info("🔍 Dry run apply completed, skipping resource readiness wait")
		return ApplyResult{
			ResourceSet: NewSimpleResourceSet(),
		}
	}

	waitTimeout := waitTimeoutFromContext(ctx, MinimalTimeout)
	if deadline, ok := ctx.Deadline(); ok {
		log.Log.Info("⏳ Using context deadline for wait timeout",
			"deadline", deadline.Format(time.RFC3339),
			"remaining", time.Until(deadline).String(),
			"timeout", waitTimeout.String())
	} else {
		log.Log.Info("⏳ Using minimal timeout", "timeout", waitTimeout.String())
	}

	// Brief in-Apply wait: snapshot the initial readiness so the bridge's
	// interim Apply ActionResult has something to send before WaitForApply
	// runs. With waitTimeout typically MinimalTimeout, this completes after
	// one tick. Resources that never reach Ready do not block here — the
	// statuspoller's Stuck classifiers and ProgressingTimeout fallback
	// ensure waitForResourcesReady always returns within a bounded duration.
	// The emitter is nil because Apply emits status via its own
	// NewActionResult path; only WaitForApply streams.
	log.Log.Info("⏳ Initial readiness snapshot")
	statuses, err := a.waitForResourcesReady(ctx, nil, objects, waitTimeout)
	if err != nil {
		log.Log.Error(err, "Some resources failed to become ready")
		// Continue even if some resources aren't ready - let WaitForApply handle it
	}

	// Get live objects to build ResourceSet
	liveObjects, err := a.getLiveObjects(ctx, objects, true)
	if err != nil {
		log.Log.Error(err, "Failed to get live objects")
		// Return with what we have
		return ApplyResult{
			LiveData:         a.liveData,
			ResourceSet:      NewSimpleResourceSet(),
			ResourceStatuses: statuses,
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
		ResourceSet:      simpleResourceSet,
		ResourceStatuses: statuses,
		Error:            nil,
	}
}

// waitTimeoutFromContext picks how long the wait phase should run.
//
// If ctx carries a deadline, return the remaining time minus a cleanup
// reserve so post-wait operations (getLiveObjects, status emission) can
// complete before the context is cancelled. The reserve degrades as the
// deadline approaches:
//
//	remaining > 15s : reserve 5s   (plenty of headroom for cleanup)
//	remaining >  5s : reserve 1s   (give the wait most of what's left)
//	otherwise       : reserve 0    (deadline-bound; let the wait have it)
//
// If ctx has no deadline, return fallback unchanged. The behaviour is
// pinned by TestWaitTimeoutFromContext.
func waitTimeoutFromContext(ctx context.Context, fallback time.Duration) time.Duration {
	deadline, ok := ctx.Deadline()
	if !ok {
		return fallback
	}
	remaining := time.Until(deadline)
	switch {
	case remaining > 15*time.Second:
		return remaining - 5*time.Second
	case remaining > 5*time.Second:
		return remaining - 1*time.Second
	default:
		return remaining
	}
}

// fmtObjMetadata formats object metadata for display
func fmtObjMetadata(obj object.ObjMetadata) string {
	if obj.Namespace != "" {
		return fmt.Sprintf("%s/%s/%s", obj.GroupKind.String(), obj.Namespace, obj.Name)
	}
	return fmt.Sprintf("%s/%s", obj.GroupKind.String(), obj.Name)
}

// aggregateReadiness reduces per-resource readiness events into an overall
// state and a human-readable summary message:
//
//	any Failed                  -> Failed
//	all Ready                   -> Ready
//	any Stuck AND no InProgress -> Stuck   (preserves InProgress when
//	                                       anything is still advancing)
//	otherwise                   -> InProgress
func aggregateReadiness(lastByID map[object.ObjMetadata]statuspoller.Event, total int) (api.ResourceReadinessType, string) {
	// Sort the iteration order so the message is stable across calls. Map
	// iteration order in Go is randomised; without this, identical inputs
	// would emit reordered status messages and trigger spurious "changed"
	// reports through the shouldEmit comparison.
	ids := make([]object.ObjMetadata, 0, len(lastByID))
	for id := range lastByID {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return fmtObjMetadata(ids[i]) < fmtObjMetadata(ids[j]) })

	var (
		anyFailed      bool
		anyFailedMsg   string
		readyCount     int
		stuckEntries   []string
		inProgressList []string
	)
	for _, id := range ids {
		ev := lastByID[id]
		name := fmtObjMetadata(id)
		switch ev.Status {
		case api.ResourceReadinessFailed:
			if anyFailed {
				continue // first sorted entry wins; deterministic.
			}
			anyFailed = true
			switch {
			case ev.Error != nil:
				anyFailedMsg = fmt.Sprintf("%s failed: %v", name, ev.Error)
			case ev.Message != "":
				anyFailedMsg = fmt.Sprintf("%s failed: %s", name, ev.Message)
			default:
				anyFailedMsg = fmt.Sprintf("%s failed", name)
			}
		case api.ResourceReadinessReady:
			readyCount++
		case api.ResourceReadinessStuck:
			if ev.Message != "" {
				stuckEntries = append(stuckEntries, fmt.Sprintf("%s: %s", name, ev.Message))
			} else {
				stuckEntries = append(stuckEntries, name)
			}
		case api.ResourceReadinessInProgress:
			inProgressList = append(inProgressList, name)
		}
	}
	switch {
	case anyFailed:
		return api.ResourceReadinessFailed, anyFailedMsg
	case total > 0 && readyCount == total:
		return api.ResourceReadinessReady, "All resources ready"
	case len(stuckEntries) > 0 && len(inProgressList) == 0:
		return api.ResourceReadinessStuck, "Stuck: " + strings.Join(stuckEntries, "; ")
	case len(stuckEntries) > 0:
		return api.ResourceReadinessInProgress, fmt.Sprintf("Reconciling: %d/%d ready; stuck: %s; in progress: %s",
			readyCount, total, strings.Join(stuckEntries, ", "), strings.Join(inProgressList, ", "))
	case len(inProgressList) > 0:
		return api.ResourceReadinessInProgress, fmt.Sprintf("Reconciling: %d/%d ready; in progress: %s",
			readyCount, total, strings.Join(inProgressList, ", "))
	default:
		return api.ResourceReadinessInProgress, fmt.Sprintf("Reconciling: %d/%d ready", readyCount, total)
	}
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

// progressEmitter publishes a Progressing ActionResult during a wait.
//
//	nil      -> silent (Apply's brief in-loop snapshot wait)
//	non-nil  -> streaming (WaitForApply keeps the server informed)
//
// The wait loop only needs SendStatus from the BridgeWorkerContext, so the
// dependency is narrowed to a function value: tests can inject a recorder
// without mocking the entire context, and the wait loop's signature names
// what it actually consumes.
type progressEmitter func(*api.ActionResult) error

// waitForResourcesReady drives the augmented status poller and aggregates
// per-resource Events into an overall api.ResourceReadinessType. When emit
// is non-nil, Progressing updates are streamed as the rollup changes or on
// heartbeat. Returns:
//
//   - the final per-resource ResourceStatusMap (nil only when there were
//     no objects to wait for); callers forward it as ApplyResult /
//     WaitResult ResourceStatuses;
//   - an error only when at least one resource is terminally Failed.
//
// Stuck and unfinished-but-progressing resources are not errors here
// (callers retry via the outer backoff loop). The status map is
// returned on every path including the failure one, so callers always
// have a snapshot to surface alongside the error.
//
// Cadence and ordering invariants live in the package-level const block at
// the top of this file and are pinned by TestCadenceInvariants. The wait
// loop never hangs silently: the statuspoller re-evaluates classifiers
// every tickInterval, and the main select heartbeats on the same tick so
// the server gets a fresh snapshot on a known cadence even with no events.
func (a *CLIUtilsApplier) waitForResourcesReady(ctx context.Context, emit progressEmitter, objects []*unstructured.Unstructured, timeout time.Duration) (api.ResourceStatusMap, error) {
	objectsMeta := object.UnstructuredSetToObjMetadataSet(objects)
	if len(objectsMeta) == 0 {
		log.Log.Info("No objects to wait for")
		return nil, nil
	}

	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	augPoller := statuspoller.New(statuspoller.Options{
		Kstatus:            a.poller,
		PollOptions:        polling.PollOptions{PollInterval: kstatusPollInterval},
		ReEvalInterval:     tickInterval,
		StuckThreshold:     stuckThreshold,
		ProgressingTimeout: a.progressingTimeout, // 0 -> statuspoller default (150s)
		Client:             a.comps.KubernetesClient,
		RESTMapper:         a.comps.RestMapper,
	})

	log.Log.Info("⏳ Starting augmented status polling", "count", len(objects))
	events := augPoller.Poll(waitCtx, objectsMeta)

	// All state below is owned by the single event-loop goroutine (no mutex).
	var (
		lastByID            = make(map[object.ObjMetadata]statuspoller.Event, len(objectsMeta))
		lastReportedState   api.ResourceReadinessType
		lastReportedMessage string
	)

	// report consumes the current event cache, computes the rollup, and
	// streams a Progressing update to the server when something changed
	// (or on heartbeat). Ready/Failed are terminal — they cancel the wait
	// loop, and the caller emits the final ActionResult after WaitForApply
	// returns with the full LiveState/Error. With emit == nil (the Apply
	// path's brief internal wait) the closure tracks rollup state for the
	// terminal-cancel check but doesn't stream anything.
	report := func(heartbeat bool) {
		rollup, message := aggregateReadiness(lastByID, len(objectsMeta))

		if rollup == api.ResourceReadinessReady || rollup == api.ResourceReadinessFailed {
			cancel()
			return
		}
		if emit == nil {
			return
		}
		if !heartbeat && rollup == lastReportedState && message == lastReportedMessage {
			return
		}
		lastReportedState = rollup
		lastReportedMessage = message
		_ = emit(progressActionResult(rollup, message,
			buildResourceStatusMap(a.comps.RestMapper, lastByID, time.Now())))
		log.Log.Info("📣 Readiness report", "state", rollup, "msg", message)
	}

	// Single event loop: consume statuspoller events and heartbeat on the
	// same select. No separate goroutine, no mutex — all state above is
	// owned by this loop.
	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()
loop:
	for {
		select {
		case <-waitCtx.Done():
			break loop
		case ev, ok := <-events:
			if !ok {
				// statuspoller closed: its ctx was cancelled (terminal state
				// reached, or outer timeout/cancel).
				break loop
			}
			lastByID[ev.Identifier] = ev
			report(false)
		case <-ticker.C:
			report(true)
		}
	}

	statuses := buildResourceStatusMap(a.comps.RestMapper, lastByID, time.Now())

	// Surface an error only when at least one resource is terminally Failed.
	// Stuck is not an error here — it is a progress report; callers can retry
	// via their outer backoff loop. Resources that simply haven't reached
	// Ready by the timeout also do not return an error; apply itself has
	// already succeeded and the caller decides how patient to be.
	if msg := failedResourcesError(lastByID, a.unitSlug); msg != "" {
		return statuses, errors.New(msg)
	}
	log.Log.Info("📊 Built per-resource status map", "count", len(statuses))
	return statuses, nil
}

// failedResourcesError formats a stable, sorted error string listing every
// resource in lastByID that is api.ResourceReadinessFailed. Returns "" when
// nothing has failed. Sort order matches aggregateReadiness's so callers see the
// same names referenced consistently between the in-flight rollup and the
// final error.
func failedResourcesError(lastByID map[object.ObjMetadata]statuspoller.Event, unit string) string {
	type entry struct {
		name, reason string
	}
	var entries []entry
	for id, ev := range lastByID {
		if ev.Status != api.ResourceReadinessFailed {
			continue
		}
		name := fmtObjMetadata(id)
		switch {
		case ev.Error != nil:
			entries = append(entries, entry{name, ev.Error.Error()})
		case ev.Message != "":
			entries = append(entries, entry{name, ev.Message})
		default:
			entries = append(entries, entry{name, "failed"})
		}
	}
	if len(entries) == 0 {
		return ""
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].name < entries[j].name })

	parts := make([]string, len(entries))
	for i, e := range entries {
		parts[i] = e.name + ": " + e.reason
	}
	return fmt.Sprintf("unit %q: %d resource(s) failed: %s", unit, len(entries), strings.Join(parts, "; "))
}

// progressActionResult is the bridge-specific translation of a non-terminal
// readiness rollup (InProgress or Stuck) into an *api.ActionResult ready to
// hand to wctx.SendStatus. Ready/Failed must be filtered out by the caller —
// terminal results carry context (LiveState, full error) that this function
// has no access to.
func progressActionResult(state api.ResourceReadinessType, message string, statuses api.ResourceStatusMap) *api.ActionResult {
	result := api.ActionResultNone
	if state == api.ResourceReadinessStuck {
		result = api.ActionResultApplyStuck
	}
	return &api.ActionResult{
		ActionResultBaseMeta: api.ActionResultBaseMeta{
			Status:  api.ActionStatusProgressing,
			Result:  result,
			Message: message,
		},
		ResourceStatuses: statuses,
	}
}

// buildResourceStatusMap projects the current event cache into the
// ResourceStatusMap returned to the bridge worker. ev.Status is already
// api.ResourceReadinessType — it is copied through directly. When the GVK
// cannot be resolved by the RESTMapper, the entry is downgraded to Unknown
// rather than dropped.
func buildResourceStatusMap(restMapper meta.RESTMapper, lastByID map[object.ObjMetadata]statuspoller.Event, now time.Time) api.ResourceStatusMap {
	out := make(api.ResourceStatusMap, len(lastByID))
	for id, ev := range lastByID {
		key, resolved := resourceStatusKey(restMapper, id)
		rs := api.ResourceStatus{
			SyncStatus: api.ResourceSyncStatusSynced,
			UpdatedAt:  now,
			Message:    ev.Message,
		}
		if !resolved {
			rs.Readiness = api.ResourceReadinessUnknown
			if rs.Message == "" {
				rs.Message = "Failed to resolve GVK"
			}
			out[key] = rs
			continue
		}
		rs.Readiness = ev.Status
		if rs.Readiness == api.ResourceReadinessFailed && rs.Message == "" && ev.Error != nil {
			rs.Message = ev.Error.Error()
		}
		if rs.Readiness == "" {
			rs.Readiness = api.ResourceReadinessUnknown
		}
		out[key] = rs
	}
	return out
}

// resourceStatusKey builds the "apiVersion/kind#namespace/name" key used in
// ResourceStatusMap. Falls back to a "group/kind#namespace/name" key (no
// version) when the RESTMapper cannot resolve the GVK — caller should treat
// that case as Unknown readiness.
func resourceStatusKey(restMapper meta.RESTMapper, id object.ObjMetadata) (funcApi.ResourceTypeAndName, bool) {
	resourceName := id.Namespace + "/" + id.Name
	mapping, err := restMapper.RESTMapping(id.GroupKind)
	if err != nil {
		rt := id.GroupKind.Kind
		if id.GroupKind.Group != "" {
			rt = id.GroupKind.Group + "/" + id.GroupKind.Kind
		}
		return funcApi.ResourceTypeAndName(rt + "#" + resourceName), false
	}
	gvk := mapping.GroupVersionKind
	rt := gvk.Version + "/" + gvk.Kind
	if gvk.Group != "" {
		rt = gvk.Group + "/" + gvk.Version + "/" + gvk.Kind
	}
	return funcApi.ResourceTypeAndName(rt + "#" + resourceName), true
}

// WaitForApply implements K8sApplier.WaitForApply.
//
// The applier is constructed fresh per call (see CLIUtilsApplier doc), so
// WaitForApply does not assume any state was carried over from the Apply
// that preceded it on the wire. The bridge worker's Apply handler returns
// before WaitForApply is dispatched, so by the time we get here Apply is
// either already on the cluster or never will be — there is nothing to
// wait for at the in-process level.
//
// Streaming: the wait loop publishes Progressing ActionResults via
// wctx.SendStatus as the rollup state changes or on heartbeats. The
// internal Apply-driven brief wait passes nil for the emitter and
// runs silently.
func (a *CLIUtilsApplier) WaitForApply(wctx api.BridgeWorkerContext, objects []*unstructured.Unstructured, timeout time.Duration) WaitResult {
	ctx := wctx.Context()
	if err := a.validate(); err != nil {
		return WaitResult{Error: err}
	}
	if err := a.validateNamespaces(objects); err != nil {
		return WaitResult{Error: err}
	}

	// Stream Progressing snapshots through wctx.SendStatus.
	statuses, err := a.waitForResourcesReady(ctx, wctx.SendStatus, objects, timeout)
	if err != nil {
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

	log.Log.Info("✅ WaitForApply completed",
		"liveObjectCount", len(liveObjects),
		"changeSetEntries", len(simpleResourceSet.GetEntries()))

	return WaitResult{
		LiveObjects:      liveObjects,
		ResourceSet:      simpleResourceSet,
		ResourceStatuses: statuses,
	}
}

// Refresh implements K8sApplier.Refresh
// Returns uncleaned live objects - caller will cleanup for LiveData, keep uncleaned for LiveState
func (a *CLIUtilsApplier) Refresh(wctx api.BridgeWorkerContext, objects []*unstructured.Unstructured) ([]*unstructured.Unstructured, error) {
	if err := a.validate(); err != nil {
		return nil, err
	}

	if err := a.validateNamespaces(objects); err != nil {
		return nil, err
	}
	// TODO: This should not return an error in the case of Not Found
	// Return uncleaned live objects - caller will cleanup for LiveData, keep uncleaned for LiveState
	return a.getLiveObjects(wctx.Context(), objects, false)
}

// Destroy implements K8sApplier.Destroy following the CLI-Utils algorithm
func (a *CLIUtilsApplier) Destroy(wctx api.BridgeWorkerContext, objects []*unstructured.Unstructured) DestroyResult {
	ctx := wctx.Context()
	log.Log.Info("🔍 Destroy called",
		"unitSlug", a.unitSlug,
		"spaceID", a.spaceID,
		"objectCount", len(objects),
		"hasInventoryCM", a.inventoryCM != nil,
		"hasInvInfo", a.invInfo != nil)

	// Step 1: Input validation
	if err := a.validate(); err != nil {
		log.Log.Error(err, "❌ Destroy validation failed")
		return DestroyResult{Error: err}
	}

	log.Log.Info("🗑️ Starting destroy operation", "unitSlug", a.unitSlug)

	// Step 2: Inventory Retrieval — Destroy only needs the inventory; it
	// holds the managed resource refs to delete. Unlike Apply, Destroy
	// cannot fabricate from input objects: with no inventory we don't know
	// what was previously applied, so deletion is a no-op at best and
	// dangerous at worst.
	invInfo, err := a.inventoryForDestroy()
	if err != nil {
		return DestroyResult{Error: err}
	}

	// Step 2.5: Back-fill inventory from input objects for legacy units that
	// were applied before inventory tracking landed.
	a.backfillEmptyInventoryFromObjects(ctx, invInfo, objects)

	// Step 3: Submit the destroy. Parallel to Apply's Step 3 (runApplyAndDrain):
	// build options, run destroyer, drain. Readiness — here, "termination" —
	// is the job of WaitForDestroy.
	if err := a.runDestroyAndDrain(ctx, invInfo); err != nil {
		return DestroyResult{Error: err}
	}

	// In dry run mode, skip waiting for resource termination.
	// Server-side dry run validates without persisting, so resources were not deleted.
	if a.dryRun {
		log.Log.Info("🔍 Dry run destroy completed, skipping resource termination wait")
		return DestroyResult{
			ResourceSet: NewSimpleResourceSet(),
		}
	}

	// Reserve headroom so the final ResourceSet emission can complete before
	// the context expires. Falls back to DefaultTimeout when no deadline.
	waitTimeout := waitTimeoutFromContext(ctx, DefaultTimeout)
	if deadline, ok := ctx.Deadline(); ok {
		log.Log.Info("⏳ Using context deadline for wait timeout",
			"deadline", deadline.Format(time.RFC3339),
			"remaining", time.Until(deadline).String(),
			"timeout", waitTimeout.String())
	} else {
		log.Log.Info("⏳ Using default timeout", "timeout", waitTimeout.String())
	}

	log.Log.Info("⏳ Waiting for resources to be terminated")
	if err := a.waitForResourcesTerminated(ctx, objects, waitTimeout); err != nil {
		log.Log.Error(err, "Some resources failed to terminate")
		// Continue even if some resources aren't terminated - let WaitForDestroy handle it
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

	log.Log.Info("✅ Destroy operation completed synchronously",
		"inventoryID", invInfo.GetID(),
		"unitSlug", a.unitSlug,
		"deletedCount", len(objects))

	return DestroyResult{
		LiveData:    nil, // No live state after destroy
		ResourceSet: simpleResourceSet,
		Error:       nil,
	}
}

// WaitForDestroy implements K8sApplier.WaitForDestroy.
//
// As with WaitForApply, the applier is fresh per call (see CLIUtilsApplier
// doc), so WaitForDestroy does not assume any state was carried over from
// the Destroy that preceded it on the wire. The handler simply polls until
// the named objects are gone or the timeout fires.
func (a *CLIUtilsApplier) WaitForDestroy(wctx api.BridgeWorkerContext, objects []*unstructured.Unstructured, timeout time.Duration) WaitResult {
	ctx := wctx.Context()
	if err := a.validate(); err != nil {
		return WaitResult{Error: err}
	}
	if err := a.validateNamespaces(objects); err != nil {
		return WaitResult{Error: err}
	}

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

	log.Log.Info("✅ WaitForDestroy completed",
		"deletedCount", len(objects),
		"changeSetEntries", len(simpleResourceSet.GetEntries()))

	return WaitResult{
		LiveObjects: []*unstructured.Unstructured{}, // No live objects after destroy
		ResourceSet: simpleResourceSet,
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

// resolveInventoryInfo decides which inventory.Info Apply will use this
// call. It prefers the inventory loaded at construction (a.inventoryCM,
// populated from BridgeState by setupApplierComponents — or from LiveData
// for legacy units predating BridgeState). When neither was available,
// it fabricates a fresh inventory ConfigMap from the first input object's
// SpaceID / UnitSlug annotations, falling back to DefaultSpaceID /
// DefaultUnitSlug when the annotations are missing or no objects were
// supplied. This is the brand-new-unit path.
//
// The returned Info is wired by inventory.ConfigMapToInventoryInfo so
// the cli-utils Applier can read and write inventory state through it.
func (a *CLIUtilsApplier) resolveInventoryInfo(objects []*unstructured.Unstructured) (inventory.Info, error) {
	if a.inventoryCM != nil && a.inventoryCM.IsValid() {
		log.Log.Info("📦 Using existing inventory", "id", a.inventoryCM.GetInventoryID())
		return a.invInfo, nil
	}

	meta := InventoryMetadata{
		SpaceID:  DefaultSpaceID,
		UnitSlug: DefaultUnitSlug,
	}
	if len(objects) > 0 {
		ann := objects[0].GetAnnotations()
		if v := ann[k8skit.SpaceIDAnnotation]; v != "" {
			meta.SpaceID = v
		} else {
			log.Log.Info("⚠️ SpaceID not found in annotations, using default")
		}
		if v := ann[k8skit.UnitSlugAnnotation]; v != "" {
			meta.UnitSlug = v
		} else {
			log.Log.Info("⚠️ UnitSlug not found in annotations, using default")
		}
	}

	normalizedSlug := k8skit.K8sNormalizeName(meta.UnitSlug)
	meta.InventoryName = fmt.Sprintf("%s-%s", InventoryPrefix, normalizedSlug)
	// Disambiguate when the same unit slug exists across spaces; a SpaceID
	// long enough to slice tells us we have a real (non-default) space.
	if meta.SpaceID != DefaultSpaceID && len(meta.SpaceID) >= 8 {
		meta.InventoryName = fmt.Sprintf("%s-%s-%s", InventoryPrefix, normalizedSlug, meta.SpaceID[:8])
	}
	meta.InventoryID = fmt.Sprintf("%s-%s", meta.SpaceID, meta.UnitSlug)

	log.Log.Info("📦 Extracted inventory metadata", "name", meta.InventoryName, "id", meta.InventoryID)
	cm := a.createInventoryConfigMap(meta)

	info, err := inventory.ConfigMapToInventoryInfo(cm)
	if err != nil {
		return nil, fmt.Errorf("failed to convert inventory ConfigMap: %w", err)
	}
	log.Log.Info("📦 Created new inventory ConfigMap", "id", meta.InventoryID)
	return info, nil
}

// inventoryConflict identifies a resource the cluster already attributes
// to a different ConfigHub inventory than the one performing this Apply.
// It carries the parsed owner identity when the inventory ID matches the
// "{spaceID}-{unitSlug}" shape ConfigHub emits, and the raw ID otherwise.
type inventoryConflict struct {
	Identifier    object.ObjMetadata
	OwnerInvID    string
	OwnerSpaceID  string
	OwnerUnitSlug string
}

// partitionByInventoryOwnership is a pre-flight pass that mirrors the
// cli-utils InventoryPolicyApplyFilter (see PolicyAdoptIfNoInventory):
// for each candidate object it fetches the live cluster object and
// places it into one of three buckets.
//
//   - toApply: the object is absent, unowned, or already owned by us
//     (cli-utils will create it or update it in place).
//   - conflicts: the live object is owned by a different inventory and
//     is not shared bridge infrastructure (see below). The caller
//     surfaces this as a single human-readable error naming the owning
//     unit instead of relying on the per-resource ApplySkipped events
//     cli-utils would emit mid-apply.
//   - excluded (third bucket, not returned because callers don't need
//     it explicitly): shared bridge infrastructure that is already in
//     place under another inventory's annotation. We remove these from
//     the apply set so cli-utils' own InventoryPolicyApplyFilter does
//     not skip them mid-apply and trip the drain. The live object
//     stays as it is — bridge-generated shared resources (e.g. the
//     argocd-oci repo-creds Secret) are deterministic per OCI host, so
//     content is identical across units.
//
// "Shared bridge infrastructure" is identified by matching
// app.kubernetes.io/managed-by labels on the desired-state object and
// the live object. The manager identity supersedes per-unit inventory
// identity for these resources.
//
// Get errors other than NotFound abort the scan: applying on a partial
// view of the cluster could mask a conflict and lead to the silent
// partial actuation we are trying to prevent.
func (a *CLIUtilsApplier) partitionByInventoryOwnership(
	ctx context.Context,
	ownInvID string,
	objects []*unstructured.Unstructured,
) (toApply []*unstructured.Unstructured, conflicts []inventoryConflict, err error) {
	for _, obj := range objects {
		key := client.ObjectKey{Namespace: obj.GetNamespace(), Name: obj.GetName()}
		current := obj.DeepCopy()
		if getErr := a.comps.KubernetesClient.Get(ctx, key, current); getErr != nil {
			if apierrors.IsNotFound(getErr) {
				toApply = append(toApply, obj)
				continue
			}
			return nil, nil, fmt.Errorf("inventory pre-flight: get %s/%s: %w",
				obj.GetNamespace(), obj.GetName(), getErr)
		}
		owner := current.GetAnnotations()[inventory.OwningInventoryKey]
		if owner == "" || owner == ownInvID {
			toApply = append(toApply, obj)
			continue
		}
		if sharedByManagedBy(obj, current) {
			// Excluded from the apply set so cli-utils does not skip
			// it and trip the drain. The live object stays as it is.
			log.Log.Info("⤴ Skipping shared bridge infrastructure (already in place under another inventory)",
				"identifier", object.UnstructuredToObjMetadata(current).String(),
				"manager", obj.GetLabels()[k8skit.LabelManagedBy])
			continue
		}
		spaceID, slug := parseInventoryID(owner)
		conflicts = append(conflicts, inventoryConflict{
			Identifier:    object.UnstructuredToObjMetadata(current),
			OwnerInvID:    owner,
			OwnerSpaceID:  spaceID,
			OwnerUnitSlug: slug,
		})
	}
	return toApply, conflicts, nil
}

// sharedByManagedBy reports whether two objects (the desired-state
// object being applied and its live counterpart) name the same
// app.kubernetes.io/managed-by value. When they do, the resource is
// shared bridge infrastructure and per-unit ownership is intentionally
// not exclusive. An empty value on either side is never a match — a
// user-authored resource without the label is still strictly checked.
func sharedByManagedBy(desired, live *unstructured.Unstructured) bool {
	d := desired.GetLabels()[k8skit.LabelManagedBy]
	if d == "" {
		return false
	}
	return d == live.GetLabels()[k8skit.LabelManagedBy]
}

// parseInventoryID splits a ConfigHub inventory ID
// ("{spaceID}-{unitSlug}", with spaceID a 36-character UUID) into its
// parts. Returns ("", invID) when the input does not match the expected
// shape, so callers can still print the raw ID as a fallback identity.
func parseInventoryID(invID string) (spaceID, unitSlug string) {
	const uuidLen = 36
	if len(invID) > uuidLen+1 && invID[uuidLen] == '-' {
		return invID[:uuidLen], invID[uuidLen+1:]
	}
	return "", invID
}

// formatInventoryConflicts renders a stable, sorted error listing every
// cross-inventory conflict. Returns nil when conflicts is empty so
// callers can use the result directly as an apply error. Sort order is
// by identifier so two runs of the same input produce the same message
// (test stability and easier diffing in CI logs).
func formatInventoryConflicts(conflicts []inventoryConflict) error {
	if len(conflicts) == 0 {
		return nil
	}
	sort.Slice(conflicts, func(i, j int) bool {
		return conflicts[i].Identifier.String() < conflicts[j].Identifier.String()
	})
	parts := make([]string, len(conflicts))
	for i, c := range conflicts {
		owner := c.OwnerInvID
		if c.OwnerSpaceID != "" && c.OwnerUnitSlug != "" {
			owner = fmt.Sprintf("unit %q in space %s", c.OwnerUnitSlug, c.OwnerSpaceID)
		}
		parts[i] = fmt.Sprintf("%s is already managed by %s", c.Identifier, owner)
	}
	return fmt.Errorf("%d resource(s) already managed by other ConfigHub units: %s",
		len(conflicts), strings.Join(parts, "; "))
}

// runApplyAndDrain executes the cli-utils Applier against the resolved
// inventory and drains the resulting event channel. It owns Apply's
// "submit" half: build SSA options, clear stale managedFields so SSA can
// take over, hand objects to the Applier, and block until the cli-utils
// internal apply task completes. The 1s ReconcileTimeout caps that
// internal wait so the drain exits promptly regardless of resource
// reconciliation — readiness is WaitForApply's job, not Apply's.
//
// Returns:
//   - ErrOperationInterrupted if ctx was cancelled mid-drain (status is
//     emitted upstream by the bridge worker, so callers should return
//     without wrapping).
//   - a wrapped error listing per-object reasons if the drain reports any
//     apply event failures.
//   - nil on success. clearManagedFields failure is non-terminal — it is
//     best-effort cleanup, logged and ignored so a transient permissions
//     glitch doesn't block the apply.
func (a *CLIUtilsApplier) runApplyAndDrain(ctx context.Context, invInfo inventory.Info, objects []*unstructured.Unstructured) error {
	applyOptions := apply.ApplierOptions{
		ServerSideOptions: common.ServerSideOptions{
			ServerSideApply: true,
			ForceConflicts:  true,
			FieldManager:    FieldManager,
		},
		ReconcileTimeout:       1 * time.Second,
		EmitStatusEvents:       true,
		NoPrune:                false,
		PruneTimeout:           DefaultTimeout,
		PrunePropagationPolicy: metav1.DeletePropagationForeground,
		InventoryPolicy:        inventory.PolicyAdoptIfNoInventory,
		DryRunStrategy:         a.dryRunStrategy(),
	}

	// Clear managedFields so SSA can take ownership cleanly: removes field
	// ownership from other managers (like kubectl) so SSA can handle array
	// item removal (e.g. removing old initContainers). Skip in dry run
	// since this mutates cluster state.
	if !a.dryRun {
		if err := a.clearManagedFieldsForObjects(ctx, objects); err != nil {
			log.Log.Error(err, "⚠️ Failed to clear managedFields, continuing with apply")
		}
	}

	log.Log.Info("📋 Starting applier with inventory", "namespace", invInfo.GetNamespace(), "id", invInfo.GetID())
	eventChannel := a.comps.Applier.Run(ctx, invInfo, objects, applyOptions)

	log.Log.Info("📋 Waiting for apply to complete by draining event channel")
	drain := drainEventChannel(ctx, eventChannel, applyEventFailure)
	if drain.contextCancelled {
		log.Log.Info("⚠️ Context cancelled while draining apply events",
			"error", ctx.Err(),
			"unitSlug", a.unitSlug)
		return ErrOperationInterrupted
	}
	log.Log.Info("📋 Apply event channel drain completed")

	if len(drain.failures) > 0 {
		return fmt.Errorf("failed to apply resources: %s", strings.Join(drain.failures, "; "))
	}
	return nil
}

// runDestroyAndDrain executes the cli-utils Destroyer against the
// resolved inventory and drains the resulting event channel. Mirrors
// runApplyAndDrain on the Destroy side: build options, hand the
// inventory to the Destroyer, drain. Termination is the job of
// WaitForDestroy, not Destroy.
//
// Returns:
//   - ErrOperationInterrupted if ctx was cancelled mid-drain (status is
//     emitted upstream by the bridge worker, so callers should return
//     without wrapping).
//   - a wrapped error listing per-object reasons if the drain reports
//     any destroy event failures.
//   - nil on success.
func (a *CLIUtilsApplier) runDestroyAndDrain(ctx context.Context, invInfo inventory.Info) error {
	log.Log.Info("🔧 Destroyer configuration",
		"DeleteTimeout", DefaultTimeout,
		"DeletePropagationPolicy", "Foreground",
		"InventoryPolicy", "AdoptIfNoInventory",
		"unitSlug", a.unitSlug)

	destroyOptions := apply.DestroyerOptions{
		DeleteTimeout:           DefaultTimeout,
		DeletePropagationPolicy: metav1.DeletePropagationForeground,
		InventoryPolicy:         inventory.PolicyAdoptIfNoInventory,
		EmitStatusEvents:        true,
		DryRunStrategy:          a.dryRunStrategy(),
	}

	log.Log.Info("📋 Starting destroyer with inventory",
		"namespace", invInfo.GetNamespace(),
		"id", invInfo.GetID(),
		"unitSlug", a.unitSlug)
	eventChannel := a.comps.Destroyer.Run(ctx, invInfo, destroyOptions)

	log.Log.Info("📋 Waiting for destroy to complete by draining event channel")
	drain := drainEventChannel(ctx, eventChannel, destroyEventFailure)
	if drain.contextCancelled {
		log.Log.Info("⚠️ Context cancelled while draining destroy events",
			"error", ctx.Err(),
			"unitSlug", a.unitSlug)
		return ErrOperationInterrupted
	}
	log.Log.Info("📋 Destroy event channel drain completed")

	if len(drain.failures) > 0 {
		return fmt.Errorf("failed to destroy resources: %s", strings.Join(drain.failures, "; "))
	}
	return nil
}

// inventoryForDestroy resolves which inventory.Info Destroy should target.
// Unlike resolveInventoryInfo (Apply), it never fabricates an inventory
// from the input objects: Destroy must know what was previously applied,
// and the input slice may be a partial list (or empty for cascading
// deletes). Without a stored inventory we error out so we never delete
// the wrong objects.
//
// Branch order matches the original Destroy logic exactly:
//
//  1. inventoryCM is valid (loaded from BridgeState or LiveData).
//  2. invInfo is set but the CM was rebuilt without IsValid passing.
//  3. neither — error.
func (a *CLIUtilsApplier) inventoryForDestroy() (inventory.Info, error) {
	if a.inventoryCM != nil && a.inventoryCM.IsValid() {
		log.Log.Info("📦 Using existing inventory for destroy",
			"id", a.inventoryCM.GetInventoryID(),
			"unitSlug", a.unitSlug)
		return a.invInfo, nil
	}
	if a.invInfo != nil {
		log.Log.Info("📦 Using existing inventory info for destroy",
			"id", a.invInfo.GetID(),
			"unitSlug", a.unitSlug)
		return a.invInfo, nil
	}
	log.Log.Error(nil, "❌ No inventory found - cannot destroy resources",
		"unitSlug", a.unitSlug,
		"spaceID", a.spaceID)
	return nil, fmt.Errorf("no inventory found - cannot destroy resources")
}

// backfillEmptyInventoryFromObjects populates the destroy inventory from
// the input objects when the stored inventory is empty. This is a
// best-effort backward-compat path for units that were applied before
// inventory tracking landed: without ObjectRefs, the cli-utils Destroyer
// has nothing to delete and would silently no-op.
//
// Failure modes are intentionally non-terminal: a Get error or a populate
// error logs a warning and returns. The destroyer will then proceed with
// whatever inventory state exists, and any orphaned objects can be cleaned
// up by a follow-up Apply that re-establishes the inventory.
//
// The InMemInventoryClient type assertion is conservative: only that
// implementation knows how to PopulateFromObjects without going through
// the cli-utils inventory protocol. Other clients fall through silently.
func (a *CLIUtilsApplier) backfillEmptyInventoryFromObjects(ctx context.Context, invInfo inventory.Info, objects []*unstructured.Unstructured) {
	if len(objects) == 0 {
		return
	}
	existingInv, err := a.comps.InventoryClient.Get(ctx, invInfo, inventory.GetOptions{})
	inventoryIsEmpty := err != nil || existingInv == nil || len(existingInv.GetObjectRefs()) == 0
	if !inventoryIsEmpty {
		return
	}

	log.Log.Info("📦 Inventory is empty, populating from input objects for backward compatibility",
		"objectCount", len(objects),
		"unitSlug", a.unitSlug)

	invClient, ok := a.comps.InventoryClient.(*InMemInventoryClient)
	if !ok {
		return
	}
	if populateErr := invClient.PopulateFromObjects(ctx, invInfo, objects); populateErr != nil {
		log.Log.Error(populateErr, "⚠️ Failed to populate inventory from objects, destroy may fail",
			"unitSlug", a.unitSlug)
		return
	}
	log.Log.Info("✅ Inventory populated from input objects",
		"count", len(objects),
		"unitSlug", a.unitSlug)
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
					FunctionAnnotation:        "inventory",
					k8skit.SpaceIDAnnotation:  metadata.SpaceID,
					k8skit.UnitSlugAnnotation: metadata.UnitSlug,
				},
			},
			"data": map[string]interface{}{},
		},
	}
}

func (a *CLIUtilsApplier) ensureConfigHubContextOnObjects(objects []*unstructured.Unstructured) {
	for _, obj := range objects {
		EnsureConfigHubContext(obj, a.unitSlug, a.spaceID, a.revisionNum)
	}
}

// validateNamespaces returns an error if any namespaced resource is missing
// metadata.namespace or has a placeholder value there, if any cluster-scoped
// resource has metadata.namespace set, or — when the applier was configured
// with an enforced namespace — if a namespaced resource's namespace does not
// match it. Callers must set metadata.namespace explicitly on every namespaced
// object and leave it unset on cluster-scoped objects — the applier no longer
// fills in a default.
func (a *CLIUtilsApplier) validateNamespaces(objects []*unstructured.Unstructured) error {
	if a.comps.KubernetesClient == nil {
		return nil
	}

	var missing, placeholder, clusterScoped, mismatched []string
	for _, obj := range objects {
		ns := obj.GetNamespace()
		ident := fmt.Sprintf("%s/%s %q", obj.GetAPIVersion(), obj.GetKind(), obj.GetName())

		isNamespaced, err := a.comps.KubernetesClient.IsObjectNamespaced(obj)
		if err != nil {
			return fmt.Errorf("failed to determine scope of %s: %w", ident, err)
		}

		switch {
		case !isNamespaced:
			if ns != "" {
				clusterScoped = append(clusterScoped, fmt.Sprintf("%s (namespace=%q)", ident, ns))
			}
		case ns == "":
			missing = append(missing, ident)
		case yamlkit.IsStringPlaceHolderValue(ns):
			placeholder = append(placeholder, fmt.Sprintf("%s (namespace=%q)", ident, ns))
		case a.enforcedNamespace != "" && ns != a.enforcedNamespace:
			mismatched = append(mismatched, fmt.Sprintf("%s (namespace=%q)", ident, ns))
		}
	}

	var errs []error
	if len(missing) > 0 {
		errs = append(errs, fmt.Errorf("metadata.namespace is required on namespaced resources but is not set on: %s",
			strings.Join(missing, ", ")))
	}
	if len(placeholder) > 0 {
		errs = append(errs, fmt.Errorf("metadata.namespace has a placeholder value on: %s",
			strings.Join(placeholder, ", ")))
	}
	if len(clusterScoped) > 0 {
		errs = append(errs, fmt.Errorf("metadata.namespace must not be set on cluster-scoped resources: %s",
			strings.Join(clusterScoped, ", ")))
	}
	if len(mismatched) > 0 {
		errs = append(errs, fmt.Errorf("metadata.namespace must equal the Target-enforced namespace %q on: %s",
			a.enforcedNamespace, strings.Join(mismatched, ", ")))
	}
	return errors.Join(errs...)
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
			Cleanup(u)
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

// buildInventoryClient selects the inventory source based on config
// priority: BridgeState (preferred) -> LiveData (legacy) -> fresh
// in-memory. The fresh branch also seeds the client with an empty
// inventory so cli-utils' Applier doesn't trip on first-time use.
//
// Parsing failures from BridgeState or LiveData fall back to fresh
// in-memory rather than propagating: a corrupt inventory blob should
// not block apply/destroy on a unit, since the next successful apply
// will re-establish it. The same is true of the empty-inventory seed
// step on the fresh branch — its errors are logged but not fatal.
//
// Inventory metadata identity is fully determined by defaultInvInfo
// (id = "<SpaceID>-<UnitSlug>"); both fields are required and validated
// by NewCLIUtilsApplier.
func buildInventoryClient(config ApplierConfig, defaultInvInfo *SimpleInventoryInfo) (inventory.Client, inventory.Info, *InventoryConfigMap) {
	if len(config.BridgeState) > 0 {
		log.Log.Info("📦 Using inventory from BridgeState")
		invClient, inventoryCM, _, err := CreateInventoryFromLiveData(context.Background(), config.BridgeState, defaultInvInfo)
		if err != nil {
			log.Log.Error(err, "⚠️ Failed to parse inventory from BridgeState, falling back to default")
			return NewInMemInventoryClient(), defaultInvInfo, NewInventoryConfigMap(defaultInvInfo)
		}
		return invClient, defaultInvInfo, inventoryCM
	}

	if len(config.LiveData) > 0 {
		// Legacy path: units created before BridgeState stored the inventory
		// ConfigMap embedded in LiveData alongside the resource manifests.
		log.Log.Info("📦 Using in-memory inventory from LiveData (legacy)")
		invClient, inventoryCM, _, err := CreateInventoryFromLiveData(context.Background(), config.LiveData, defaultInvInfo)
		if err != nil {
			log.Log.Error(err, "⚠️ Failed to create inventory from LiveData, falling back to default")
			return NewInMemInventoryClient(), defaultInvInfo, NewInventoryConfigMap(defaultInvInfo)
		}
		return invClient, defaultInvInfo, inventoryCM
	}

	log.Log.Info("📦 Using standard in-memory inventory")
	invClient := NewInMemInventoryClient()
	inventoryCM := NewInventoryConfigMapWithOptions(defaultInvInfo, InventoryOptions{
		SpaceID:  config.SpaceID,
		UnitSlug: config.UnitSlug,
	})
	seedEmptyInventory(invClient, defaultInvInfo)
	return invClient, defaultInvInfo, inventoryCM
}

// seedEmptyInventory creates an empty inventory in the client so cli-utils'
// Applier doesn't fail on first-time use looking for a missing inventory
// object. Errors are logged but not propagated: a seeding failure leaves
// the client in the same state as before, and the first apply will retry
// inventory creation.
func seedEmptyInventory(invClient inventory.Client, invInfo inventory.Info) {
	ctx := context.TODO()
	newInv, err := invClient.NewInventory(invInfo)
	if err != nil {
		log.Log.Error(err, "Failed to create initial inventory")
		return
	}
	newInv.SetObjectRefs(object.ObjMetadataSet{})
	if err := invClient.CreateOrUpdate(ctx, newInv, inventory.UpdateOptions{}); err != nil {
		log.Log.Error(err, "Failed to initialize inventory in client")
		return
	}
	log.Log.Info("📦 Initialized empty inventory in client", "id", invInfo.GetID())
}

// setupApplierComponents wires the cluster-side client tower
// (rest.Config -> controller-runtime client -> dynamic client -> fresh
// discovery -> DeferredDiscoveryRESTMapper -> kubectl factory) and the
// cli-utils Applier/Destroyer atop a fresh inventory client.
//
// The discovery client is wrapped in FreshDiscoveryClient so newly
// installed CRDs are observable on the next ServerGroupsAndResources
// call (see waitForCRDsAvailable). The factory uses SimpleRESTClientGetter
// to avoid kubectl's default disk discovery cache, which fails on
// read-only filesystems (e.g. Kubernetes pods).
func setupApplierComponents(config ApplierConfig) (*ApplierComponents, inventory.Info, *InventoryConfigMap, error) {
	// Initialize klog for CLI-Utils debugging (only once)
	initKlog()

	// Create default inventory info — uses constant name since the inventory
	// ConfigMap is an in-memory artifact stored in BridgeState, not applied
	// to the cluster.
	defaultInvInfo := &SimpleInventoryInfo{
		namespace: DefaultNamespace,
		name:      InventoryConfigMapName,
		id:        fmt.Sprintf("%s-%s", config.SpaceID, config.UnitSlug),
	}

	cfg, err := KubernetesConfigFactory(config.KubeContext)
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
	discoveryClient, err := discovery.NewDiscoveryClientForConfig(cfg)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to create discovery client: %w", err)
	}
	freshDiscovery := &FreshDiscoveryClient{DiscoveryClient: discoveryClient}
	restMapper := restmapper.NewDeferredDiscoveryRESTMapper(freshDiscovery)
	factory := util.NewFactory(&SimpleRESTClientGetter{
		restConfig: cfg,
		restMapper: restMapper,
	})

	invClient, invInfo, inventoryCM := buildInventoryClient(config, defaultInvInfo)

	applier, err := apply.NewApplierBuilder().
		WithFactory(factory).
		WithInventoryClient(invClient).
		Build()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to create applier: %w", err)
	}

	destroyer, err := apply.NewDestroyerBuilder().
		WithFactory(factory).
		WithInventoryClient(invClient).
		Build()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to create destroyer: %w", err)
	}

	log.Log.Info("✅ Created applier components", "hasBridgeState", len(config.BridgeState) > 0, "hasLiveData", len(config.LiveData) > 0)

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

// NewCLIUtilsApplier creates a new K8sApplier instance.
//
// SpaceID and UnitSlug are hard preconditions: the inventory ConfigMap's
// identity is derived from them, so missing either would either collide
// with other units or produce a malformed id (e.g. a bare "-"). The
// bridge worker should never invoke the applier without both set; we
// fail loudly here rather than fabricate an id from KubeContext (which
// is worker-local and frequently empty in-cluster).
func NewCLIUtilsApplier(config ApplierConfig) (K8sApplier, error) {
	if config.SpaceID == "" {
		return nil, fmt.Errorf("CLIUtilsApplier requires SpaceID")
	}
	if config.UnitSlug == "" {
		return nil, fmt.Errorf("CLIUtilsApplier requires UnitSlug")
	}

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
		comps:              comps,
		liveData:           config.LiveData,
		spaceID:            config.SpaceID,
		unitSlug:           config.UnitSlug,
		revisionNum:        config.RevisionNum,
		waitTimeout:        config.WaitTimeout,
		enforcedNamespace:  config.EnforcedNamespace,
		inventoryCM:        inventoryCM,
		invInfo:            invInfo,
		liveDataBuilder:    liveDataBuilder,
		poller:             poller,
		dryRun:             config.DryRun,
		progressingTimeout: config.ProgressingTimeout,
	}, nil
}

// waitForCRDsAvailable blocks until every CRD-backed GVK in objects has
// been registered in API discovery, or the per-call timeout elapses. Core
// (no-group) types are skipped — they're always available.
//
// This guards against the install-and-apply race: a unit that ships a
// CRD plus a CR of that CRD's kind would otherwise fail the CR apply
// before the CRD is observable. Backing off lets a controller (e.g.
// Crossplane) finish installing while we keep refreshing discovery.
func (a *CLIUtilsApplier) waitForCRDsAvailable(ctx context.Context, objects []*unstructured.Unstructured) error {
	gvkSet := requiredCRDsForObjects(objects)
	if len(gvkSet) == 0 {
		log.Log.Info("No CRDs to check - all resources are core types")
		return nil
	}
	log.Log.Info("Checking CRD availability", "count", len(gvkSet))

	checkCtx, cancel := context.WithTimeout(ctx, crdCheckTimeout(a.waitTimeout))
	defer cancel()

	for retryCount := 0; ; retryCount++ {
		if checkCtx.Err() != nil {
			return fmt.Errorf("timeout waiting for CRDs to be available")
		}

		missing := findMissingCRDs(a.comps.DiscoveryClient, gvkSet)
		if len(missing) == 0 {
			log.Log.Info("✅ All required CRDs are available")
			return nil
		}

		delay := crdBackoffDelay(retryCount)
		log.Log.Info("⏳ Waiting for CRDs to be available",
			"missing", missing,
			"retry", retryCount+1,
			"nextCheck", delay)

		select {
		case <-time.After(delay):
		case <-checkCtx.Done():
			return fmt.Errorf("timeout waiting for CRDs: %v", missing)
		}
	}
}

// requiredCRDsForObjects returns the set of GVKs from objects that need
// a CRD, keyed by "group/version/kind". Core (empty-group) types are
// excluded — they ship with the apiserver. Duplicates are coalesced.
func requiredCRDsForObjects(objects []*unstructured.Unstructured) map[string]schema.GroupVersionKind {
	gvkSet := make(map[string]schema.GroupVersionKind)
	for _, obj := range objects {
		gvk := obj.GroupVersionKind()
		if gvk.Group == "" {
			continue
		}
		key := fmt.Sprintf("%s/%s/%s", gvk.Group, gvk.Version, gvk.Kind)
		gvkSet[key] = gvk
	}
	return gvkSet
}

// crdCheckTimeout picks the upper bound on the CRD-availability wait,
// preferring the configured WaitTimeout and falling back to
// LargeWaitTimeout on empty or unparseable input. Parse failures are
// logged but non-fatal: a misconfigured timeout shouldn't block apply.
func crdCheckTimeout(waitTimeout string) time.Duration {
	if waitTimeout == "" {
		return LargeWaitTimeout
	}
	d, err := time.ParseDuration(waitTimeout)
	if err != nil {
		log.Log.Info("Invalid WaitTimeout, using LargeWaitTimeout for CRD checks", "timeout", waitTimeout, "error", err)
		return LargeWaitTimeout
	}
	log.Log.Info("Using WaitTimeout for CRD checks", "timeout", d)
	return d
}

// findMissingCRDs refreshes discovery (if the client supports it) and
// returns the keys of GVKs that are not yet registered. A partial
// discovery error is logged at V(2) and treated as "scan what we got" —
// transient discovery glitches shouldn't fail the wait, since the
// retry loop will re-scan on the next tick.
func findMissingCRDs(discoveryClient discovery.CachedDiscoveryInterface, gvkSet map[string]schema.GroupVersionKind) []string {
	if freshDiscovery, ok := discoveryClient.(*FreshDiscoveryClient); ok {
		freshDiscovery.Invalidate()
	}
	_, apiResourceList, err := discoveryClient.ServerGroupsAndResources()
	if err != nil {
		log.Log.V(2).Info("Discovery error (continuing)", "error", err)
	}

	missing := []string{}
	for key, gvk := range gvkSet {
		if !gvkRegistered(apiResourceList, gvk) {
			missing = append(missing, key)
		}
	}
	return missing
}

// gvkRegistered reports whether gvk appears in apiResourceList. Group/
// version match is exact; kind match is case-sensitive.
func gvkRegistered(apiResourceList []*metav1.APIResourceList, gvk schema.GroupVersionKind) bool {
	for _, apiResource := range apiResourceList {
		gv, err := schema.ParseGroupVersion(apiResource.GroupVersion)
		if err != nil {
			continue
		}
		if gv.Group != gvk.Group || gv.Version != gvk.Version {
			continue
		}
		for _, resource := range apiResource.APIResources {
			if resource.Kind == gvk.Kind {
				return true
			}
		}
	}
	return false
}

// crdBackoffDelay returns the wait between CRD-availability checks for
// the given attempt number (0-indexed): 2s, 4s, 8s, 16s, 32s, then
// capped at 30s. The cap matches kubectl wait's behaviour and bounds
// worst-case noise on long installations.
func crdBackoffDelay(retryCount int) time.Duration {
	switch {
	case retryCount == 0:
		return 2 * time.Second
	case retryCount < 5:
		return time.Duration(1<<uint(retryCount)) * time.Second
	default:
		return 30 * time.Second
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
