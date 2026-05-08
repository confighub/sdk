// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package statuspoller

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/confighub/sdk/core/worker/api"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/cli-utils/pkg/kstatus/status"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const testThreshold = 30 * time.Second

func newObj(apiVersion, kind, ns, name string) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	u.SetAPIVersion(apiVersion)
	u.SetKind(kind)
	u.SetNamespace(ns)
	u.SetName(name)
	return u
}

func setField(u *unstructured.Unstructured, value interface{}, path ...string) {
	_ = unstructured.SetNestedField(u.Object, value, path...)
}

func setCondition(u *unstructured.Unstructured, condType, condStatus, reason, message string) {
	conds, _, _ := unstructured.NestedSlice(u.Object, "status", "conditions")
	conds = append(conds, map[string]interface{}{
		"type":    condType,
		"status":  condStatus,
		"reason":  reason,
		"message": message,
	})
	_ = unstructured.SetNestedSlice(u.Object, conds, "status", "conditions")
}

func mustInput(obj *unstructured.Unstructured, kstate status.Status, since time.Duration) ClassifierInput {
	now := time.Now()
	return ClassifierInput{
		Object:       obj,
		KState:       kstate,
		LastChangeAt: now.Add(-since),
		Now:          now,
	}
}

func ctxCC() (context.Context, *ClassifierContext) {
	return context.Background(), &ClassifierContext{StuckThreshold: testThreshold}
}

// makeOwnedApp builds an Argo Application owned by the given parent UID. When
// autoSyncOn is false the Application has no spec.syncPolicy.automated and is
// therefore Stuck per the Application classifier; when true it has automated
// set and is treated as healthy.
func makeOwnedApp(ns, name, parentUID string, autoSyncOn bool) *unstructured.Unstructured {
	app := newObj("argoproj.io/v1alpha1", "Application", ns, name)
	app.SetOwnerReferences([]metav1.OwnerReference{{
		APIVersion: "argoproj.io/v1alpha1",
		Kind:       "ApplicationSet",
		Name:       "irrelevant",
		UID:        types.UID(parentUID),
	}})
	if autoSyncOn {
		_ = unstructured.SetNestedMap(app.Object, map[string]interface{}{}, "spec", "syncPolicy", "automated")
	}
	return app
}

// stubListClient is a minimal Client implementation for unit tests that returns
// a fixed slice of objects from List and refuses Get. Callers using it must
// not invoke Get on it; they must invoke only List on UnstructuredList types.
type stubListClient struct {
	items []unstructured.Unstructured
}

func (s *stubListClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	return errors.New("stubListClient: Get not implemented")
}

func (s *stubListClient) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	ul, ok := list.(*unstructured.UnstructuredList)
	if !ok {
		return errors.New("stubListClient: only UnstructuredList supported")
	}
	ul.Items = append(ul.Items, s.items...)
	return nil
}

// Generic fallback.

func TestGeneric_NotStuckBelowThreshold(t *testing.T) {
	obj := newObj("apps/v1", "Deployment", "ns", "n")
	setField(obj, int64(2), "metadata", "generation")
	setField(obj, int64(1), "status", "observedGeneration")
	ctx, cc := ctxCC()
	st, _ := Generic(ctx, cc, mustInput(obj, status.InProgressStatus, 10*time.Second))
	if st != "" {
		t.Fatalf("should not be stuck below threshold, got %q", st)
	}
}

func TestGeneric_StuckOnGenMismatchAfterThreshold(t *testing.T) {
	obj := newObj("apps/v1", "Deployment", "ns", "n")
	setField(obj, int64(2), "metadata", "generation")
	setField(obj, int64(1), "status", "observedGeneration")
	ctx, cc := ctxCC()
	st, reason := Generic(ctx, cc, mustInput(obj, status.InProgressStatus, testThreshold+time.Second))
	if st != api.ResourceReadinessStuck {
		t.Fatalf("should be stuck past threshold with generation lag, got %q", st)
	}
	if reason == "" {
		t.Fatalf("reason should be non-empty")
	}
}

func TestGeneric_NotStuckWhenGenMatches(t *testing.T) {
	obj := newObj("apps/v1", "Deployment", "ns", "n")
	setField(obj, int64(2), "metadata", "generation")
	setField(obj, int64(2), "status", "observedGeneration")
	ctx, cc := ctxCC()
	st, _ := Generic(ctx, cc, mustInput(obj, status.InProgressStatus, testThreshold+time.Second))
	if st != "" {
		t.Fatalf("should not be stuck when observedGeneration == generation, got %q", st)
	}
}

func TestGeneric_NotStuckWithoutObservedGeneration(t *testing.T) {
	obj := newObj("v1", "ConfigMap", "ns", "n")
	ctx, cc := ctxCC()
	st, _ := Generic(ctx, cc, mustInput(obj, status.InProgressStatus, testThreshold+time.Second))
	if st != "" {
		t.Fatalf("should not be stuck when object has no observedGeneration field, got %q", st)
	}
}

// Flux Kustomization / HelmRelease.

func TestFluxKustomization_SuspendedImmediatelyStuck(t *testing.T) {
	obj := newObj("kustomize.toolkit.fluxcd.io/v1", "Kustomization", "flux-system", "k")
	setField(obj, true, "spec", "suspend")
	ctx, cc := ctxCC()
	st, reason := FluxKustomization(ctx, cc, mustInput(obj, status.InProgressStatus, 0))
	if st != api.ResourceReadinessStuck || reason == "" {
		t.Fatalf("suspended Kustomization must be stuck immediately, got status=%q reason=%q", st, reason)
	}
}

func TestFluxKustomization_StalledCondition(t *testing.T) {
	obj := newObj("kustomize.toolkit.fluxcd.io/v1", "Kustomization", "flux-system", "k")
	setCondition(obj, "Stalled", "True", "BuildFailed", "kustomize build failed: ...")
	ctx, cc := ctxCC()
	st, reason := FluxKustomization(ctx, cc, mustInput(obj, status.InProgressStatus, 0))
	if st != api.ResourceReadinessStuck {
		t.Fatalf("Stalled=True must be stuck, got %q", st)
	}
	if reason == "" {
		t.Fatalf("reason should include Stalled signal")
	}
}

func TestFluxKustomization_GenMismatchTimeout(t *testing.T) {
	obj := newObj("kustomize.toolkit.fluxcd.io/v1", "Kustomization", "flux-system", "k")
	setField(obj, int64(1), "metadata", "generation")
	// observedGeneration intentionally missing
	ctx, cc := ctxCC()
	st, _ := FluxKustomization(ctx, cc, mustInput(obj, status.InProgressStatus, testThreshold+time.Second))
	if st != api.ResourceReadinessStuck {
		t.Fatalf("observedGeneration missing past threshold should be stuck, got %q", st)
	}
}

func TestFluxHelmRelease_SuspendedStuck(t *testing.T) {
	obj := newObj("helm.toolkit.fluxcd.io/v2", "HelmRelease", "flux-system", "h")
	setField(obj, true, "spec", "suspend")
	ctx, cc := ctxCC()
	st, _ := FluxHelmRelease(ctx, cc, mustInput(obj, status.InProgressStatus, 0))
	if st != api.ResourceReadinessStuck {
		t.Fatalf("suspended HelmRelease should be stuck, got %q", st)
	}
}

// Job.

func TestJob_SuspendedStuck(t *testing.T) {
	obj := newObj("batch/v1", "Job", "ns", "j")
	setField(obj, true, "spec", "suspend")
	ctx, cc := ctxCC()
	st, _ := Job(ctx, cc, mustInput(obj, status.InProgressStatus, 0))
	if st != api.ResourceReadinessStuck {
		t.Fatalf("suspended Job should be stuck, got %q", st)
	}
}

func TestJob_NotSuspendedNotStuck(t *testing.T) {
	obj := newObj("batch/v1", "Job", "ns", "j")
	ctx, cc := ctxCC()
	st, _ := Job(ctx, cc, mustInput(obj, status.InProgressStatus, 0))
	if st != "" {
		t.Fatalf("non-suspended Job should pass through, got %q", st)
	}
}

// CRD.

func TestCRD_NamesAcceptedFalseImmediatelyStuck(t *testing.T) {
	obj := newObj("apiextensions.k8s.io/v1", "CustomResourceDefinition", "", "foo.example.com")
	setCondition(obj, "NamesAccepted", "False", "Conflict", "conflict with existing CRD")
	ctx, cc := ctxCC()
	st, reason := CRD(ctx, cc, mustInput(obj, status.InProgressStatus, 0))
	if st != api.ResourceReadinessStuck || reason == "" {
		t.Fatalf("CRD NamesAccepted=False should be stuck, got status=%q reason=%q", st, reason)
	}
}

func TestCRD_EstablishedFalseAfterThreshold(t *testing.T) {
	obj := newObj("apiextensions.k8s.io/v1", "CustomResourceDefinition", "", "foo.example.com")
	setCondition(obj, "Established", "False", "NotAccepted", "names not yet accepted")
	ctx, cc := ctxCC()
	st, _ := CRD(ctx, cc, mustInput(obj, status.InProgressStatus, testThreshold+time.Second))
	if st != api.ResourceReadinessStuck {
		t.Fatalf("CRD Established=False past threshold should be stuck, got %q", st)
	}
}

func TestCRD_EstablishedFalseBelowThresholdNotStuck(t *testing.T) {
	obj := newObj("apiextensions.k8s.io/v1", "CustomResourceDefinition", "", "foo.example.com")
	setCondition(obj, "Established", "False", "NotAccepted", "")
	ctx, cc := ctxCC()
	st, _ := CRD(ctx, cc, mustInput(obj, status.InProgressStatus, 5*time.Second))
	if st != "" {
		t.Fatalf("CRD Established=False below threshold should not yet be stuck, got %q", st)
	}
}

// ArgoCD Application.

func TestApplication_NoSyncPolicyIsStuck(t *testing.T) {
	obj := newObj("argoproj.io/v1alpha1", "Application", "argocd", "a")
	ctx, cc := ctxCC()
	st, reason := Application(ctx, cc, mustInput(obj, status.InProgressStatus, testThreshold+time.Second))
	if st != api.ResourceReadinessStuck || reason == "" {
		t.Fatalf("Application with no syncPolicy.automated should be stuck, got status=%q reason=%q", st, reason)
	}
}

func TestApplication_NoSyncPolicyBelowThresholdNotStuck(t *testing.T) {
	// The renderer flow applies an Application without auto-sync just to
	// harvest rendered manifests as LiveState. Within StuckThreshold the
	// classifier must hold its verdict so the renderer has time to read.
	obj := newObj("argoproj.io/v1alpha1", "Application", "argocd", "a")
	ctx, cc := ctxCC()
	st, _ := Application(ctx, cc, mustInput(obj, status.InProgressStatus, 0))
	if st != "" {
		t.Fatalf("Application with no syncPolicy.automated below threshold should not yet be stuck, got %q", st)
	}
}

func TestApplication_AutomatedEnabledFalseIsStuck(t *testing.T) {
	obj := newObj("argoproj.io/v1alpha1", "Application", "argocd", "a")
	_ = unstructured.SetNestedMap(obj.Object, map[string]interface{}{"enabled": false}, "spec", "syncPolicy", "automated")
	ctx, cc := ctxCC()
	st, _ := Application(ctx, cc, mustInput(obj, status.InProgressStatus, testThreshold+time.Second))
	if st != api.ResourceReadinessStuck {
		t.Fatalf("Application with automated.enabled=false should be stuck, got %q", st)
	}
}

func TestApplication_AutomatedPresentNotStuck(t *testing.T) {
	obj := newObj("argoproj.io/v1alpha1", "Application", "argocd", "a")
	// automated: {} (present, enabled not explicitly false) → autosync on
	_ = unstructured.SetNestedMap(obj.Object, map[string]interface{}{}, "spec", "syncPolicy", "automated")
	ctx, cc := ctxCC()
	st, _ := Application(ctx, cc, mustInput(obj, status.InProgressStatus, 0))
	if st == api.ResourceReadinessStuck {
		t.Fatalf("Application with autosync enabled should not be stuck")
	}
}

func TestApplication_ComparisonErrorStuck(t *testing.T) {
	obj := newObj("argoproj.io/v1alpha1", "Application", "argocd", "a")
	_ = unstructured.SetNestedMap(obj.Object, map[string]interface{}{"enabled": true}, "spec", "syncPolicy", "automated")
	setCondition(obj, "ComparisonError", "True", "", "cannot compare to repo")
	ctx, cc := ctxCC()
	st, reason := Application(ctx, cc, mustInput(obj, status.InProgressStatus, 0))
	if st != api.ResourceReadinessStuck || reason == "" {
		t.Fatalf("ComparisonError=True should be stuck, got status=%q reason=%q", st, reason)
	}
}

// ApplicationSet.

func TestApplicationSet_ErrorOccurredStuck(t *testing.T) {
	obj := newObj("argoproj.io/v1alpha1", "ApplicationSet", "argocd", "as")
	setCondition(obj, "ErrorOccurred", "True", "BadGenerator", "generator failed")
	ctx, cc := ctxCC()
	st, _ := ApplicationSet(ctx, cc, mustInput(obj, status.InProgressStatus, 0))
	if st != api.ResourceReadinessStuck {
		t.Fatalf("ErrorOccurred=True must be stuck, got %q", st)
	}
}

func TestApplicationSet_CleanIsNotStuck(t *testing.T) {
	// No conditions set; no client → classifier passes through.
	obj := newObj("argoproj.io/v1alpha1", "ApplicationSet", "argocd", "as")
	ctx, cc := ctxCC()
	st, _ := ApplicationSet(ctx, cc, mustInput(obj, status.InProgressStatus, 0))
	if st != "" {
		t.Fatalf("ApplicationSet with no error conditions and no client should pass through, got %q", st)
	}
}

// TestApplicationSet_AllChildrenStuck verifies the recursive aggregation:
// when every owned Application is itself Stuck (here: auto-sync off on each),
// the parent ApplicationSet flips Stuck. Crucially, this fires on the FIRST
// poll — not gated by StuckThreshold — because ApplicationSet's kstate is
// Current immediately and the bridge applier would otherwise exit before
// the gate elapsed.
func TestApplicationSet_AllChildrenStuck(t *testing.T) {
	parent := newObj("argoproj.io/v1alpha1", "ApplicationSet", "argocd", "as")
	parent.SetUID("parent-uid")
	child1 := makeOwnedApp("argocd", "c1", "parent-uid", false)
	child2 := makeOwnedApp("argocd", "c2", "parent-uid", false)

	cc := &ClassifierContext{StuckThreshold: testThreshold, Client: &stubListClient{items: []unstructured.Unstructured{*child1, *child2}}}
	// since=0 — no time has elapsed; gate-removed branch must still fire.
	st, reason := ApplicationSet(context.Background(), cc, mustInput(parent, status.InProgressStatus, 0))
	if st != api.ResourceReadinessStuck {
		t.Fatalf("ApplicationSet with all children stuck must be Stuck, got %q", st)
	}
	if !strings.Contains(reason, "all stuck") {
		t.Fatalf("expected reason to mention 'all stuck', got %q", reason)
	}
}

// TestApplicationSet_MixedChildrenIsNotStuck verifies the aggregation gate:
// one healthy child must save the Set even if all the others are stuck.
func TestApplicationSet_MixedChildrenIsNotStuck(t *testing.T) {
	parent := newObj("argoproj.io/v1alpha1", "ApplicationSet", "argocd", "as")
	parent.SetUID("parent-uid")
	stuckChild := makeOwnedApp("argocd", "c-stuck", "parent-uid", false)
	healthyChild := makeOwnedApp("argocd", "c-ok", "parent-uid", true)

	cc := &ClassifierContext{StuckThreshold: testThreshold, Client: &stubListClient{items: []unstructured.Unstructured{*stuckChild, *healthyChild}}}
	st, _ := ApplicationSet(context.Background(), cc, mustInput(parent, status.InProgressStatus, 0))
	if st == api.ResourceReadinessStuck {
		t.Fatalf("ApplicationSet with one healthy child must not be Stuck")
	}
}

// TestApplicationSet_NoOwnedChildrenIsNotStuck — children that don't have an
// ownerReference back to the Set don't count. Empty owned set short-circuits.
func TestApplicationSet_NoOwnedChildrenIsNotStuck(t *testing.T) {
	parent := newObj("argoproj.io/v1alpha1", "ApplicationSet", "argocd", "as")
	parent.SetUID("parent-uid")
	// Stuck Application but owned by a different UID — must be ignored.
	other := makeOwnedApp("argocd", "other", "different-uid", false)

	cc := &ClassifierContext{StuckThreshold: testThreshold, Client: &stubListClient{items: []unstructured.Unstructured{*other}}}
	st, _ := ApplicationSet(context.Background(), cc, mustInput(parent, status.InProgressStatus, 0))
	if st == api.ResourceReadinessStuck {
		t.Fatalf("ApplicationSet with no owned children must not be Stuck")
	}
}

// Registry pass-through.

func TestRegistry_CurrentPassesThrough(t *testing.T) {
	obj := newObj("apps/v1", "Deployment", "ns", "n")
	reg := DefaultRegistry()
	ctx, cc := ctxCC()
	st, _ := reg.Classify(ctx, cc, mustInput(obj, status.CurrentStatus, 0))
	if st != api.ResourceReadinessReady {
		t.Fatalf("Current must pass through as Ready, got %s", st)
	}
}

func TestRegistry_FailedPassesThrough(t *testing.T) {
	obj := newObj("apps/v1", "Deployment", "ns", "n")
	reg := DefaultRegistry()
	ctx, cc := ctxCC()
	st, _ := reg.Classify(ctx, cc, mustInput(obj, status.FailedStatus, 0))
	if st != api.ResourceReadinessFailed {
		t.Fatalf("Failed must pass through, got %s", st)
	}
}

// Kind classifiers must run even when kstatus says Current. kstatus's Current
// for a CRD without standard conditions is a "no Ready=False so assume fine"
// fallback, not an authoritative signal. Per-kind rules have explicit Stuck
// knowledge that must win. Regression test for the smoke-test gap where an
// ArgoCD Application with auto-sync disabled was silently classified Ready.

func TestRegistry_KindClassifierRunsEvenWhenKStatusCurrent(t *testing.T) {
	// ArgoCD Application with no syncPolicy.automated. kstatus default for a
	// CRD with empty status is Current. Without the reordering, the kind
	// classifier would never fire and the Application would be called Ready.
	// Past StuckThreshold so the auto-sync gate has elapsed.
	obj := newObj("argoproj.io/v1alpha1", "Application", "argocd", "app")
	reg := DefaultRegistry()
	ctx, cc := ctxCC()
	st, reason := reg.Classify(ctx, cc, mustInput(obj, status.CurrentStatus, testThreshold+time.Second))
	if st != api.ResourceReadinessStuck {
		t.Fatalf("auto-sync-disabled Application must be Stuck regardless of kstatus, got %s", st)
	}
	if !strings.Contains(reason, "auto-sync") {
		t.Fatalf("expected auto-sync reason, got %q", reason)
	}
}

func TestRegistry_KindClassifierPassThroughHonoursKStatusCurrent(t *testing.T) {
	// If the kind classifier passes through (not stuck) and kstatus says
	// Current, the final verdict is Ready. No regression for happy-path
	// Applications with auto-sync enabled.
	obj := newObj("argoproj.io/v1alpha1", "Application", "argocd", "app")
	_ = unstructured.SetNestedMap(obj.Object, map[string]interface{}{}, "spec", "syncPolicy", "automated")
	reg := DefaultRegistry()
	ctx, cc := ctxCC()
	st, _ := reg.Classify(ctx, cc, mustInput(obj, status.CurrentStatus, 0))
	if st != api.ResourceReadinessReady {
		t.Fatalf("auto-sync-enabled Application with kstatus=Current must be Ready, got %s", st)
	}
}

func TestRegistry_KStatusFailedAlwaysWins(t *testing.T) {
	// Failed is authoritative. A kind classifier cannot mask it even if it
	// would have said Stuck.
	obj := newObj("argoproj.io/v1alpha1", "Application", "argocd", "app")
	// no syncPolicy.automated — classifier WOULD say Stuck if it ran
	reg := DefaultRegistry()
	ctx, cc := ctxCC()
	st, _ := reg.Classify(ctx, cc, mustInput(obj, status.FailedStatus, 0))
	if st != api.ResourceReadinessFailed {
		t.Fatalf("Failed must win over kind classifier, got %s", st)
	}
}

func TestRegistry_UnknownKindFallsToGeneric(t *testing.T) {
	obj := newObj("example.com/v1", "Thing", "ns", "n")
	setField(obj, int64(5), "metadata", "generation")
	setField(obj, int64(1), "status", "observedGeneration")
	reg := DefaultRegistry()
	ctx, cc := ctxCC()
	st, _ := reg.Classify(ctx, cc, mustInput(obj, status.InProgressStatus, testThreshold+time.Second))
	if st != api.ResourceReadinessStuck {
		t.Fatalf("unknown kind with gen-mismatch-timeout should fall to Generic Stuck, got %s", st)
	}
}

// ProgressingTimeout fallback: when no kind-specific or generic rule flags
// Stuck, the Registry's time-based fallback must elevate InProgress to Stuck
// once elapsed ≥ ProgressingTimeout.

const testProgressingTimeout = 150 * time.Second

func ctxCCWithProgTimeout() (context.Context, *ClassifierContext) {
	return context.Background(), &ClassifierContext{
		StuckThreshold:     testThreshold,
		ProgressingTimeout: testProgressingTimeout,
	}
}

func TestRegistry_ProgressingTimeoutElevatesInProgress(t *testing.T) {
	// Kind with no classifier match and no observedGeneration mismatch: the
	// generic rule stays silent and only the ProgressingTimeout fallback fires.
	obj := newObj("v1", "Service", "ns", "svc")
	reg := DefaultRegistry()
	ctx, cc := ctxCCWithProgTimeout()
	st, reason := reg.Classify(ctx, cc, mustInput(obj, status.InProgressStatus, testProgressingTimeout+time.Second))
	if st != api.ResourceReadinessStuck {
		t.Fatalf("InProgress beyond ProgressingTimeout must be Stuck, got %s", st)
	}
	if reason == "" {
		t.Fatalf("ProgressingTimeout fallback must include a reason")
	}
}

func TestRegistry_ProgressingTimeoutNotTriggeredBelowThreshold(t *testing.T) {
	obj := newObj("v1", "Service", "ns", "svc")
	reg := DefaultRegistry()
	ctx, cc := ctxCCWithProgTimeout()
	st, _ := reg.Classify(ctx, cc, mustInput(obj, status.InProgressStatus, testProgressingTimeout-time.Second))
	if st != api.ResourceReadinessInProgress {
		t.Fatalf("below ProgressingTimeout must stay InProgress, got %s", st)
	}
}

func TestRegistry_ProgressingTimeoutDisabledWhenZero(t *testing.T) {
	obj := newObj("v1", "Service", "ns", "svc")
	reg := DefaultRegistry()
	ctx, cc := ctxCC() // ProgressingTimeout = 0 → disabled
	st, _ := reg.Classify(ctx, cc, mustInput(obj, status.InProgressStatus, 24*time.Hour))
	if st != api.ResourceReadinessInProgress {
		t.Fatalf("ProgressingTimeout=0 must disable fallback, got %s", st)
	}
}

func TestRegistry_KindClassifierTakesPrecedenceOverProgressingTimeout(t *testing.T) {
	// Suspended Flux Kustomization: the kind classifier fires immediately, and
	// its reason (not the generic "progressing for ≥ Xs") must be what the
	// registry reports.
	obj := newObj("kustomize.toolkit.fluxcd.io/v1", "Kustomization", "flux-system", "ks")
	setField(obj, true, "spec", "suspend")
	reg := DefaultRegistry()
	ctx, cc := ctxCCWithProgTimeout()
	st, reason := reg.Classify(ctx, cc, mustInput(obj, status.InProgressStatus, testProgressingTimeout+time.Second))
	if st != api.ResourceReadinessStuck {
		t.Fatalf("expected Stuck, got %s", st)
	}
	if reason == "" {
		t.Fatalf("expected non-empty reason")
	}
	// The kind-specific reason mentions "suspended"; the fallback would say
	// "Progressing for ≥". If we see the fallback wording, precedence is wrong.
	if strings.Contains(reason, "Progressing for") {
		t.Fatalf("kind classifier reason was overridden by fallback: %q", reason)
	}
}
