// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package statuspoller

import (
	"context"
	"sync"
	"time"

	"github.com/confighub/sdk/core/worker/api"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/cli-utils/pkg/kstatus/polling"
	pollingevent "sigs.k8s.io/cli-utils/pkg/kstatus/polling/event"
	"sigs.k8s.io/cli-utils/pkg/object"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Event is the augmented kstatus event the Poller emits downstream.
type Event struct {
	Identifier object.ObjMetadata
	Status     api.ResourceReadinessType
	Message    string
	// Resource is the object as kstatus last observed it. May be nil on the
	// first emit if we haven't yet received an upstream event.
	Resource *pollingevent.ResourceStatus
	Error    error
}

// Options configures the augmented poller.
type Options struct {
	// Kstatus is the wrapped kstatus poller.
	Kstatus *polling.StatusPoller

	// PollOptions is forwarded to Kstatus.Poll.
	PollOptions polling.PollOptions

	// ReEvalInterval drives the reclassification ticker. When kstatus is
	// silent, classifiers are re-run every ReEvalInterval to catch Stuck
	// transitions that depend on elapsed time (e.g. Deployment image pull
	// taking too long). Default: 5s.
	ReEvalInterval time.Duration

	// StuckThreshold is passed to classifiers that use timeout-based fallback
	// logic (generic, generation-mismatch timeouts). Default: 30s.
	StuckThreshold time.Duration

	// ProgressingTimeout is the last-resort time-based Stuck fallback applied
	// by Registry.Classify when no kind-specific or generic rule fires.
	// Default: 150s. Must be larger than StuckThreshold so kind-specific
	// classifiers can produce a precise reason first.
	ProgressingTimeout time.Duration

	// Client and RESTMapper are given to classifiers that need to fetch
	// related objects (e.g. pods for a Deployment classifier).
	Client     Client
	RESTMapper meta.RESTMapper

	// Classifiers overrides or extends the default kind registry.
	Classifiers Registry
}

// Poller wraps kstatus and applies per-kind Stuck classifiers.
type Poller struct {
	opts Options
}

// refreshResource fetches a fresh copy of the resource described by prev and
// returns a new *pollingevent.ResourceStatus carrying the refreshed object.
// kstatus's Status/Message/Error are preserved as-is — recomputing kstate is
// kstatus's job, not ours, and we don't want to second-guess what cli-utils
// last decided. Returns nil if the client is unavailable, the prior resource
// is unknown, or the live Get fails — caller falls back to the stale entry.
//
// Why this exists: cli-utils dedupes kstatus events via ResourceStatusEqual,
// which compares only Status/Message/Generation. A custom-condition flip that
// doesn't change kstate (e.g. ACK.ResourceSynced going True while Ready stays
// missing) emits no kstatus event, so without this refresh classifiers would
// keep running on stale conditions and never see the transition.
func refreshResource(ctx context.Context, c Client, prev *pollingevent.ResourceStatus) *pollingevent.ResourceStatus {
	if c == nil || prev == nil || prev.Resource == nil {
		return nil
	}
	gvk := prev.Resource.GroupVersionKind()
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(gvk)
	key := client.ObjectKey{Namespace: prev.Resource.GetNamespace(), Name: prev.Resource.GetName()}
	if err := c.Get(ctx, key, obj); err != nil {
		return nil
	}
	cp := *prev
	cp.Resource = obj
	return &cp
}

// hasProgress reports whether a new kstatus event carries genuinely new
// information about the resource compared to the last event we saw.
//
// Returning true means "the controller (or someone) has written to the
// resource since the last observation" — either a new resourceVersion, a
// different kstatus classification, or a different status message. This is
// the signal the poller uses to keep the progressing-timeout clock alive
// during long-but-progressing provisions (e.g. a Deployment slowly pulling
// large images and advancing replicas one by one).
//
// resourceVersion is the broadest "controller wrote something" signal we
// have. The classifier layer above runs on the same kstatus event, so it
// sees the new resourceVersion implicitly through the unstructured object
// it inspects. We don't need the *classifier* to react to a resourceVersion
// bump — what matters here is that the *clock* doesn't tick toward Stuck
// while a controller is demonstrably making writes. So even if the
// classifier's verdict didn't change between two events, an advancing
// resourceVersion still resets LastChangeAt so ProgressingTimeout
// doesn't false-positive on a slow-but-active provision.
//
// A nil prev is treated as progress so the first observation always seeds
// the clock cleanly.
func hasProgress(prev, cur *pollingevent.ResourceStatus) bool {
	if prev == nil {
		return true
	}
	if cur == nil {
		return false
	}
	if prev.Status != cur.Status {
		return true
	}
	if prev.Message != cur.Message {
		return true
	}
	if prev.Resource == nil || cur.Resource == nil {
		return prev.Resource != cur.Resource
	}
	return prev.Resource.GetResourceVersion() != cur.Resource.GetResourceVersion()
}

// New constructs a Poller. Defaults fill in for ReEvalInterval, StuckThreshold,
// ProgressingTimeout, and Classifiers.
//
// The intervals must satisfy ReEvalInterval < StuckThreshold < ProgressingTimeout
// so:
//   - kind classifiers running on the re-eval ticker get to fire before the
//     grace period (StuckThreshold) ends and timeout-based rules kick in;
//   - kind-specific Stuck reasons always win over the generic
//     ProgressingTimeout fallback.
//
// New clamps any value that would violate the chain up to the next floor,
// instead of taking it at face value, so a misconfigured caller never
// silently gets an unworkable cadence.
func New(opts Options) *Poller {
	if opts.ReEvalInterval <= 0 {
		opts.ReEvalInterval = 5 * time.Second
	}
	if opts.StuckThreshold <= opts.ReEvalInterval {
		// Use a multiple of ReEvalInterval so the time-based classifiers see
		// at least a few re-evals before they may flag Stuck.
		opts.StuckThreshold = 6 * opts.ReEvalInterval
	}
	if opts.ProgressingTimeout <= opts.StuckThreshold {
		opts.ProgressingTimeout = 5 * opts.StuckThreshold
	}
	if opts.Classifiers.ByKind == nil && opts.Classifiers.Fallbacks == nil {
		opts.Classifiers = DefaultRegistry()
	}
	return &Poller{opts: opts}
}

// Poll runs until ctx is cancelled. The returned channel is closed when
// polling stops. The poller emits an Event for each resource whenever its
// augmented status changes — this includes transitions driven by kstatus and
// those driven by the reclassification ticker.
func (p *Poller) Poll(ctx context.Context, ids object.ObjMetadataSet) <-chan Event {
	out := make(chan Event, len(ids))

	kstatusCh := p.opts.Kstatus.Poll(ctx, ids, p.opts.PollOptions)

	cc := &ClassifierContext{
		Client:             p.opts.Client,
		RESTMapper:         p.opts.RESTMapper,
		StuckThreshold:     p.opts.StuckThreshold,
		ProgressingTimeout: p.opts.ProgressingTimeout,
	}

	// Per-resource cached augmented state.
	var mu sync.Mutex
	lastEmitted := make(map[object.ObjMetadata]api.ResourceReadinessType)
	lastChange := make(map[object.ObjMetadata]time.Time)
	lastRaw := make(map[object.ObjMetadata]*pollingevent.ResourceStatus)

	// classifyAndMaybeEmit runs the classifier for the given (id, raw) and
	// emits to out when the augmented status has changed. Caller holds mu.
	//
	// Seeds lastChange[id] = now on first observation so the classifier sees
	// zero elapsed time for new resources — this prevents grace-period checks
	// (like Deployment's pod inspection) from firing instantly on first sight.
	classifyAndMaybeEmit := func(id object.ObjMetadata, raw *pollingevent.ResourceStatus, now time.Time) {
		if _, seen := lastChange[id]; !seen {
			lastChange[id] = now
		}
		in := ClassifierInput{
			Object:       nil,
			KState:       "",
			LastChangeAt: lastChange[id],
			Now:          now,
		}
		if raw != nil {
			in.Object = raw.Resource
			in.KState = raw.Status
		}
		st, reason := p.opts.Classifiers.Classify(ctx, cc, in)
		msg := reason
		if msg == "" && raw != nil {
			msg = raw.Message
		}
		prev, had := lastEmitted[id]
		if had && prev == st {
			return
		}
		if had && prev != st {
			// Augmented status transition: reset the grace-period clock.
			// (On first emission, lastChange was just seeded above.)
			lastChange[id] = now
		}
		lastEmitted[id] = st
		ev := Event{
			Identifier: id,
			Status:     st,
			Message:    msg,
			Resource:   raw,
		}
		if raw != nil {
			ev.Error = raw.Error
		}
		select {
		case out <- ev:
		case <-ctx.Done():
		}
	}

	go func() {
		defer close(out)
		ticker := time.NewTicker(p.opts.ReEvalInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return

			case e, ok := <-kstatusCh:
				if !ok {
					// kstatus closed — keep the ticker alive until ctx is
					// cancelled so Stuck transitions can still fire.
					kstatusCh = nil
					continue
				}
				if e.Type != pollingevent.ResourceUpdateEvent || e.Resource == nil {
					continue
				}
				mu.Lock()
				// Reset the progressing-timeout clock whenever the raw event
				// carries genuinely new information about the resource
				// (resourceVersion advance, status flip, or message change).
				// Without this, a long-but-progressing install (e.g. a Flux
				// Deployment slowly pulling images) would trip the timeout
				// even while the controller is clearly advancing status.
				prev := lastRaw[e.Resource.Identifier]
				if hasProgress(prev, e.Resource) {
					lastChange[e.Resource.Identifier] = time.Now()
				}
				lastRaw[e.Resource.Identifier] = e.Resource
				classifyAndMaybeEmit(e.Resource.Identifier, e.Resource, time.Now())
				mu.Unlock()

			case <-ticker.C:
				now := time.Now()
				mu.Lock()
				for id, raw := range lastRaw {
					// Refresh the cached Resource via a direct Get before
					// reclassifying. kstatus dedupes events on its own
					// (Status/Message/Generation only — see
					// cli-utils ResourceStatusEqual), so a status.conditions
					// update that doesn't change kstate (e.g. an ACK
					// controller writing ACK.ResourceSynced=True while Ready
					// stays absent) never reaches us through kstatusCh. We do
					// our own refresh here so per-kind classifiers can react
					// to custom-condition transitions in finite time.
					if fresh := refreshResource(ctx, p.opts.Client, raw); fresh != nil {
						if hasProgress(raw, fresh) {
							lastChange[id] = now
						}
						raw = fresh
						lastRaw[id] = fresh
					}
					classifyAndMaybeEmit(id, raw, now)
				}
				mu.Unlock()
			}
		}
	}()

	return out
}
