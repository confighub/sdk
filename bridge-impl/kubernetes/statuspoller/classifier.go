// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package statuspoller

import (
	"context"
	"fmt"
	"time"

	"github.com/confighub/sdk/core/worker/api"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/cli-utils/pkg/kstatus/status"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Client is the minimal subset of sigs.k8s.io/controller-runtime/client.Client
// that classifiers and the augmented poller need. It is satisfied by the full
// Client as well as by the applier's internal KubernetesClient interface.
type Client interface {
	Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error
	List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error
}

// kstatusToReadiness maps a raw kstatus.Status to api.ResourceReadinessType.
// Stuck has no kstatus equivalent — only classifiers can elevate to Stuck.
var kstatusToReadiness = map[status.Status]api.ResourceReadinessType{
	status.CurrentStatus:     api.ResourceReadinessReady,
	status.InProgressStatus:  api.ResourceReadinessInProgress,
	status.FailedStatus:      api.ResourceReadinessFailed,
	status.TerminatingStatus: api.ResourceReadinessTerminating,
}

// FromKstatus maps a raw kstatus.Status to api.ResourceReadinessType.
// Unknown kstatus values map to ResourceReadinessUnknown.
func FromKstatus(s status.Status) api.ResourceReadinessType {
	if r, ok := kstatusToReadiness[s]; ok {
		return r
	}
	return api.ResourceReadinessUnknown
}

// isTerminalKstate reports whether s is a kstatus state the controller has
// authoritatively written (Failed, Terminating). The classifier never
// overrides these — Stuck is only a refinement of InProgress / Current.
func isTerminalKstate(s status.Status) bool {
	return s == status.FailedStatus || s == status.TerminatingStatus
}

// ClassifierContext gives a classifier the tools it needs to look beyond the
// resource object itself (e.g. a Deployment classifier fetching its pods).
type ClassifierContext struct {
	Client         Client
	RESTMapper     meta.RESTMapper
	StuckThreshold time.Duration // grace period before timeout-based Stuck fires
	// ProgressingTimeout is the last-resort fallback applied by Registry.Classify
	// after all kind-specific and generic rules pass. When >0, a resource whose
	// kstate is InProgress and whose LastChangeAt is older than
	// ProgressingTimeout is elevated to Stuck. Must be larger than
	// StuckThreshold so kind-specific classifiers get first say.
	ProgressingTimeout time.Duration
}

// ClassifierInput is the per-resource input to a classifier.
type ClassifierInput struct {
	// Object is the unstructured form of the resource as observed by kstatus.
	Object *unstructured.Unstructured
	// KState is what kstatus currently computes for the object.
	KState status.Status
	// LastChangeAt is when this resource's augmented status last changed.
	LastChangeAt time.Time
	// Now is the wall-clock time at the moment of classification.
	Now time.Time
}

// Classifier inspects a resource and decides its augmented readiness.
//
// Contract:
//   - Returns ("", "") to pass through to the next layer (next Fallback,
//     kstatus.Current, ProgressingTimeout, or kstatus passthrough). A
//     non-empty status is definitive — no later layer overrides it.
//   - Reason must be non-empty whenever status is.
//   - Must be monotonic in LastChangeAt: once a classifier returns Stuck for
//     a given (Object, KState), returning Stuck must continue to hold as time
//     advances until the input changes.
//   - Must not return Ready when KState is Failed or Terminating — those
//     authoritative kstates are handled before any classifier runs.
//
// Classifiers that need to fetch related objects should use the provided
// client with a bounded context.
type Classifier func(ctx context.Context, cc *ClassifierContext, in ClassifierInput) (status api.ResourceReadinessType, reason string)

// Registry decides which classifier handles which resource.
//
//   - ByKind is checked first. An exact GroupKind match runs only its own
//     classifier; Fallbacks are skipped on a hit (the kind has bespoke rules
//     and is responsible for delegating to Generic if it wants the
//     gen-mismatch fallback — see e.g. the Application classifier).
//   - Fallbacks run in order when no ByKind entry matched. Each may return a
//     definitive status or pass through. First non-empty wins. Typical
//     contents: a group-suffix matcher like ACK, then Generic.
type Registry struct {
	ByKind    map[schema.GroupKind]Classifier
	Fallbacks []Classifier
}

// Classify decides the augmented status for a resource.
//
// Order of precedence:
//
//  1. Failed and Terminating pass through. Those are authoritative signals
//     kstatus only returns when the controller has actively written terminal
//     condition data.
//  2. ByKind exact-match classifier runs and, if it returns a definitive
//     status, that wins. Runs for both InProgress and Current — kstatus's
//     CurrentStatus on a CRD without standard conditions is a "no Ready=False
//     so assume fine" fallback, not authoritative, and per-kind rules may
//     have explicit Stuck knowledge that must override it.
//  3. If no ByKind entry matched, Fallbacks run in order. The default chain
//     is [ACK, Generic]: ACK handles *.services.k8s.aws by group suffix and
//     can return either Ready (ACK.ResourceSynced=True) or Stuck
//     (ACK.Terminal=True); Generic handles observedGeneration lag.
//  4. kstatus's Current pass-through wins if no rule had an opinion.
//  5. ProgressingTimeout last-resort fallback, only for InProgress.
//  6. Otherwise pass through kstatus's verdict.
func (r Registry) Classify(ctx context.Context, cc *ClassifierContext, in ClassifierInput) (api.ResourceReadinessType, string) {
	// Authoritative terminal states pass through. Classifiers cannot override
	// Failed or Terminating — those are written by controllers, not guessed.
	if isTerminalKstate(in.KState) {
		return FromKstatus(in.KState), ""
	}

	if in.Object == nil {
		// No object to classify; pass through whatever kstatus said.
		return FromKstatus(in.KState), ""
	}

	gk := in.Object.GroupVersionKind().GroupKind()
	if fn, ok := r.ByKind[gk]; ok {
		if st, reason := fn(ctx, cc, in); st != "" {
			return st, reason
		}
	} else {
		for _, fn := range r.Fallbacks {
			if st, reason := fn(ctx, cc, in); st != "" {
				return st, reason
			}
		}
	}

	// kstatus's Current pass-through wins only after classifiers have had
	// their say.
	if in.KState == status.CurrentStatus {
		return api.ResourceReadinessReady, ""
	}

	// Progressing-timeout fallback: if no kind-specific or generic rule has
	// flagged Stuck, but the resource has been in InProgress for longer than
	// ProgressingTimeout without any controller signal, escalate to Stuck so
	// the user sees a transition rather than a silent wait. The rule runs last
	// so kind-specific reasons always take precedence. Monotone in LastChangeAt:
	// once (Now - LastChangeAt) ≥ ProgressingTimeout, it keeps holding until
	// LastChangeAt advances.
	if cc.ProgressingTimeout > 0 &&
		in.KState == status.InProgressStatus &&
		in.Now.Sub(in.LastChangeAt) >= cc.ProgressingTimeout {
		return api.ResourceReadinessStuck, fmt.Sprintf("resource has been Progressing for ≥ %s with no controller signal", cc.ProgressingTimeout)
	}

	return FromKstatus(in.KState), ""
}

// DefaultRegistry returns the built-in Registry covering the kinds where we
// have meaningful stuck signals. Callers may extend it.
func DefaultRegistry() Registry {
	return Registry{
		ByKind: map[schema.GroupKind]Classifier{
			{Group: "apps", Kind: "Deployment"}:                               Deployment,
			{Group: "apps", Kind: "StatefulSet"}:                              StatefulSet,
			{Group: "apps", Kind: "DaemonSet"}:                                DaemonSet,
			{Group: "batch", Kind: "Job"}:                                     Job,
			{Group: "kustomize.toolkit.fluxcd.io", Kind: "Kustomization"}:     FluxKustomization,
			{Group: "helm.toolkit.fluxcd.io", Kind: "HelmRelease"}:            FluxHelmRelease,
			{Group: "argoproj.io", Kind: "Application"}:                       Application,
			{Group: "argoproj.io", Kind: "ApplicationSet"}:                    ApplicationSet,
			{Group: "apiextensions.k8s.io", Kind: "CustomResourceDefinition"}: CRD,
		},
		Fallbacks: []Classifier{ACK, Generic},
	}
}
