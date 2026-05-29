// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package kubernetes

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/confighub/sdk/bridge-impl/kubernetes/statuspoller"
	"github.com/confighub/sdk/configkit/k8skit"
	"github.com/confighub/sdk/core/worker/api"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/cli-utils/pkg/apis/actuation"
	"sigs.k8s.io/cli-utils/pkg/apply"
	"sigs.k8s.io/cli-utils/pkg/apply/event"
	"sigs.k8s.io/cli-utils/pkg/inventory"
	"sigs.k8s.io/cli-utils/pkg/object"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// TestCadenceInvariants pins the ordering relationships between the wait-loop
// cadence constants. Violating any of these silently breaks the Stuck
// detection chain — see the doc comment above the const block for the
// reasoning behind each rule.
func TestCadenceInvariants(t *testing.T) {
	if !(kstatusPollInterval <= tickInterval) {
		t.Fatalf("kstatusPollInterval (%s) must be <= tickInterval (%s); otherwise classifiers re-evaluate on stale kstatus state",
			kstatusPollInterval, tickInterval)
	}
	if !(tickInterval < stuckThreshold) {
		t.Fatalf("tickInterval (%s) must be < stuckThreshold (%s); otherwise time-based classifiers may not fire before the grace period",
			tickInterval, stuckThreshold)
	}
	// statuspoller's default ProgressingTimeout, when callers don't set one,
	// is 5*stuckThreshold. The kind-specific stuckThreshold must be strictly
	// less so the per-kind reason wins over the generic fallback.
	defaultProgressingTimeout := 5 * stuckThreshold
	if !(stuckThreshold < defaultProgressingTimeout) {
		t.Fatalf("stuckThreshold (%s) must be < default progressingTimeout (%s)",
			stuckThreshold, defaultProgressingTimeout)
	}
	if !(tickInterval <= heartbeatInterval) {
		t.Fatalf("tickInterval (%s) must be <= heartbeatInterval (%s); otherwise the heartbeat would tick faster than the wait loop and the rate limit is meaningless",
			tickInterval, heartbeatInterval)
	}
}

// TestWaitTimeoutFromContext pins the deadline-reserve tiers used by Apply
// to size its in-Apply wait. The actual numbers (5s/1s reserves at the 15s
// and 5s thresholds) are tuned for the bridge worker's getLiveObjects
// follow-up; locking them prevents accidental drift.
func TestWaitTimeoutFromContext(t *testing.T) {
	tcs := []struct {
		name      string
		remaining time.Duration
		want      time.Duration
	}{
		{"plenty: reserve 5s", 60 * time.Second, 55 * time.Second},
		{"comfortably above 15s: reserve 5s", 30 * time.Second, 25 * time.Second},
		{"between 5s and 15s: reserve 1s", 10 * time.Second, 9 * time.Second},
		{"comfortably above 5s: reserve 1s", 8 * time.Second, 7 * time.Second},
		{"below 5s: reserve 0", 2 * time.Second, 2 * time.Second},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(tc.remaining))
			defer cancel()
			got := waitTimeoutFromContext(ctx, 99*time.Hour)
			// Allow ~200ms slop because time.Until ticks while the call runs.
			if got > tc.want || tc.want-got > 200*time.Millisecond {
				t.Fatalf("remaining=%s: want ~%s, got %s", tc.remaining, tc.want, got)
			}
		})
	}

	t.Run("no deadline returns fallback", func(t *testing.T) {
		got := waitTimeoutFromContext(context.Background(), 7*time.Second)
		if got != 7*time.Second {
			t.Fatalf("no-deadline ctx: want fallback 7s, got %s", got)
		}
	})
}

// TestComputeRollupDeterministic guarantees the rollup message is stable
// across runs given identical inputs. Map iteration in Go is randomised, so
// without explicit sorting the message would flicker and trigger spurious
// "changed" reports through the shouldEmit comparison.
func TestComputeRollupDeterministic(t *testing.T) {
	mk := func(ns, name string, st api.ResourceReadinessType, msg string) (object.ObjMetadata, statuspoller.Event) {
		id := object.ObjMetadata{
			Namespace: ns,
			Name:      name,
			GroupKind: schema.GroupKind{Group: "apps", Kind: "Deployment"},
		}
		return id, statuspoller.Event{Identifier: id, Status: st, Message: msg}
	}

	build := func() map[object.ObjMetadata]statuspoller.Event {
		m := map[object.ObjMetadata]statuspoller.Event{}
		for _, p := range []struct {
			ns, name, msg string
			st            api.ResourceReadinessType
		}{
			{"default", "alpha", "blocked", api.ResourceReadinessStuck},
			{"default", "bravo", "blocked", api.ResourceReadinessStuck},
			{"default", "charlie", "", api.ResourceReadinessInProgress},
			{"default", "delta", "", api.ResourceReadinessReady},
		} {
			id, ev := mk(p.ns, p.name, p.st, p.msg)
			m[id] = ev
		}
		return m
	}

	const runs = 16
	want := ""
	for i := 0; i < runs; i++ {
		_, got := aggregateReadiness(build(), 4)
		if i == 0 {
			want = got
			continue
		}
		if got != want {
			t.Fatalf("aggregateReadiness is non-deterministic across runs:\n run 0: %q\n run %d: %q", want, i, got)
		}
	}
}

// TestFailedResourcesError pins the contract of the trailing wait-loop
// error: present-tense unit reference, count, and sorted resource list.
// The format is parsed by humans reading logs; reorderings would obscure
// regressions in apply behaviour.
func TestFailedResourcesError(t *testing.T) {
	mk := func(name, errMsg string) (object.ObjMetadata, statuspoller.Event) {
		id := object.ObjMetadata{
			Namespace: "default",
			Name:      name,
			GroupKind: schema.GroupKind{Group: "apps", Kind: "Deployment"},
		}
		return id, statuspoller.Event{Identifier: id, Status: api.ResourceReadinessFailed, Error: errors.New(errMsg)}
	}
	m := map[object.ObjMetadata]statuspoller.Event{}
	for _, p := range []struct{ name, err string }{
		{"zulu", "z-error"},
		{"alpha", "a-error"},
		{"mike", "m-error"},
		{"healthy", ""},
	} {
		if p.err == "" {
			id := object.ObjMetadata{Namespace: "default", Name: p.name, GroupKind: schema.GroupKind{Group: "apps", Kind: "Deployment"}}
			m[id] = statuspoller.Event{Identifier: id, Status: api.ResourceReadinessReady}
			continue
		}
		id, ev := mk(p.name, p.err)
		m[id] = ev
	}

	got := failedResourcesError(m, "demo-unit")
	want := `unit "demo-unit": 3 resource(s) failed: Deployment.apps/default/alpha: a-error; Deployment.apps/default/mike: m-error; Deployment.apps/default/zulu: z-error`
	if got != want {
		t.Fatalf("failedResourcesError mismatch\n want: %s\n got:  %s", want, got)
	}

	if failedResourcesError(map[object.ObjMetadata]statuspoller.Event{}, "u") != "" {
		t.Fatalf("empty input must yield empty error")
	}
}

// TestComputeRollupFailedPicksFirstSorted ensures that when multiple
// resources are Failed, the rollup names the lexicographically first one
// (by formatted ObjMetadata) so callers see a stable failure message.
func TestComputeRollupFailedPicksFirstSorted(t *testing.T) {
	mk := func(ns, name string, st api.ResourceReadinessType, errMsg string) (object.ObjMetadata, statuspoller.Event) {
		id := object.ObjMetadata{
			Namespace: ns,
			Name:      name,
			GroupKind: schema.GroupKind{Group: "apps", Kind: "Deployment"},
		}
		ev := statuspoller.Event{Identifier: id, Status: st}
		if errMsg != "" {
			ev.Error = errors.New(errMsg)
		}
		return id, ev
	}

	m := map[object.ObjMetadata]statuspoller.Event{}
	for _, p := range []struct {
		ns, name, errMsg string
	}{
		{"default", "zulu", "z-error"},
		{"default", "alpha", "a-error"},
		{"default", "mike", "m-error"},
	} {
		id, ev := mk(p.ns, p.name, api.ResourceReadinessFailed, p.errMsg)
		m[id] = ev
	}

	state, msg := aggregateReadiness(m, 3)
	if state != api.ResourceReadinessFailed {
		t.Fatalf("rollup state: want Failed, got %q", state)
	}
	if !strings.Contains(msg, "alpha") {
		t.Fatalf("rollup message must surface the lexicographically first failed resource (alpha); got %q", msg)
	}
	if strings.Contains(msg, "zulu") || strings.Contains(msg, "mike") {
		t.Fatalf("rollup message must name only the first failure; got %q", msg)
	}
}

// TestValidate pins the dependency-check contract that gates every public
// method on CLIUtilsApplier (Apply, WaitForApply, Refresh, Destroy,
// WaitForDestroy). Each branch returns a distinct, stable error string so
// callers and operators can tell which component is missing without
// re-reading the source.
func TestValidate(t *testing.T) {
	mockKC := &MockK8sClient{}
	tcs := []struct {
		name  string
		comps *ApplierComponents
		want  string // empty means no error expected
	}{
		{"nil components", nil, "dependencies not initialized"},
		{"missing applier", &ApplierComponents{KubernetesClient: mockKC}, "applier not initialized"},
		{"missing kubernetes client", &ApplierComponents{Applier: &apply.Applier{}}, "kubernetes client not initialized"},
		{"all wired", &ApplierComponents{Applier: &apply.Applier{}, KubernetesClient: mockKC}, ""},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			a := &CLIUtilsApplier{comps: tc.comps}
			err := a.validate()
			if tc.want == "" {
				if err != nil {
					t.Fatalf("validate(): want nil, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("validate(): want %q, got nil", tc.want)
			}
			if err.Error() != tc.want {
				t.Fatalf("validate(): want %q, got %q", tc.want, err.Error())
			}
		})
	}
}

// TestDrainEventChannel_ChannelClose: the helper consumes every event the
// sender produces and returns a clean drainResult once the channel closes.
// This is the happy path Apply / Destroy depend on for a successful run.
func TestDrainEventChannel_ChannelClose(t *testing.T) {
	ch := make(chan event.Event, 4)
	ch <- event.Event{Type: event.InitType}
	ch <- event.Event{Type: event.StatusType}
	close(ch)

	r := drainEventChannel(context.Background(), ch, func(event.Event) string { return "" })

	if r.contextCancelled {
		t.Fatalf("contextCancelled = true; expected false on clean close")
	}
	if len(r.failures) != 0 {
		t.Fatalf("failures = %v; expected none when predicate returns \"\"", r.failures)
	}
}

// TestDrainEventChannel_AccumulatesFailures pins the contract that
// failureFor returning a non-empty string causes the value to land in
// drainResult.failures, in the order the events arrived. Apply / Destroy
// rely on this ordering when joining the messages into a terminal error.
func TestDrainEventChannel_AccumulatesFailures(t *testing.T) {
	ch := make(chan event.Event, 4)
	ch <- event.Event{Type: event.InitType}                        // ignored
	ch <- event.Event{Type: event.ErrorType}                       // -> "first"
	ch <- event.Event{Type: event.StatusType}                      // ignored
	ch <- event.Event{Type: event.ApplyType}                       // -> "second"
	close(ch)

	calls := 0
	pred := func(e event.Event) string {
		switch e.Type {
		case event.ErrorType:
			calls++
			return "first"
		case event.ApplyType:
			calls++
			return "second"
		}
		return ""
	}

	r := drainEventChannel(context.Background(), ch, pred)

	if r.contextCancelled {
		t.Fatalf("contextCancelled = true; expected false")
	}
	want := []string{"first", "second"}
	if len(r.failures) != len(want) {
		t.Fatalf("failures = %v; want %v", r.failures, want)
	}
	for i, msg := range want {
		if r.failures[i] != msg {
			t.Fatalf("failures[%d] = %q; want %q", i, r.failures[i], msg)
		}
	}
	if calls != 2 {
		t.Fatalf("predicate calls = %d; want 2 (one per non-zero return)", calls)
	}
}

// TestDrainEventChannel_ContextCancel pins the cancellation contract:
// when ctx fires, drainEventChannel sets contextCancelled, returns
// promptly, and forks a background goroutine that keeps draining the
// channel so the cli-utils sender does not block on send. The test
// proves the latter by sending more events after cancellation and
// closing the channel — if the background drain were missing, the
// test goroutine would deadlock on send.
func TestDrainEventChannel_ContextCancel(t *testing.T) {
	ch := make(chan event.Event)
	ctx, cancel := context.WithCancel(context.Background())

	type result struct {
		r drainResult
	}
	done := make(chan result, 1)
	go func() {
		done <- result{r: drainEventChannel(ctx, ch, func(event.Event) string { return "" })}
	}()

	cancel()
	select {
	case got := <-done:
		if !got.r.contextCancelled {
			t.Fatalf("contextCancelled = false; want true after ctx cancel")
		}
		if len(got.r.failures) != 0 {
			t.Fatalf("failures = %v; want none", got.r.failures)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("drainEventChannel did not return within 2s after ctx cancel")
	}

	// Background drain must still be reading the channel — these sends
	// would block (and the goroutine close() would deadlock the test)
	// without it.
	send := make(chan struct{})
	go func() {
		ch <- event.Event{Type: event.StatusType}
		ch <- event.Event{Type: event.StatusType}
		close(ch)
		close(send)
	}()
	select {
	case <-send:
	case <-time.After(2 * time.Second):
		t.Fatalf("background drain did not consume post-cancel events; sender deadlocked")
	}
}

// TestApplyEventFailure pins which apply events are terminal failures.
// ErrorType, ApplyFailed-with-error, and ApplySkipped contribute
// messages; everything else (including PruneFailed, which is logged but
// not terminal, and ApplySuccessful) returns "". ApplySkipped is
// terminal because cli-utils only emits it when a filter rejected the
// apply — the resource was not actuated, so the bridge's intent was
// not fulfilled and the caller must see a non-empty error.
func TestApplyEventFailure(t *testing.T) {
	apiErr := errors.New("network blip")
	skipErr := &inventory.PolicyPreventedActuationError{
		Strategy: actuation.ActuationStrategyApply,
		Policy:   inventory.PolicyAdoptIfNoInventory,
		Status:   inventory.NoMatch,
	}
	skipID := object.ObjMetadata{
		GroupKind: schema.GroupKind{Kind: "Namespace"},
		Name:      "shared-ns",
	}
	tcs := []struct {
		name string
		ev   event.Event
		want string
	}{
		{"ErrorType returns the error string", event.Event{Type: event.ErrorType, ErrorEvent: event.ErrorEvent{Err: apiErr}}, "network blip"},
		{"ApplyFailed with error contributes a failure", event.Event{Type: event.ApplyType, ApplyEvent: event.ApplyEvent{Status: event.ApplyFailed, Error: apiErr}}, ": network blip"},
		{"ApplySkipped contributes a failure carrying the filter reason", event.Event{Type: event.ApplyType, ApplyEvent: event.ApplyEvent{Status: event.ApplySkipped, Identifier: skipID, Error: skipErr}}, "shared-ns"},
		{"ApplySuccessful is silent", event.Event{Type: event.ApplyType, ApplyEvent: event.ApplyEvent{Status: event.ApplySuccessful}}, ""},
		{"PruneFailed is logged, not terminal", event.Event{Type: event.PruneType, PruneEvent: event.PruneEvent{Status: event.PruneFailed, Error: apiErr}}, ""},
		{"InitType is silent", event.Event{Type: event.InitType}, ""},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			got := applyEventFailure(tc.ev)
			if !strings.Contains(got, tc.want) {
				t.Fatalf("applyEventFailure: want substring %q, got %q", tc.want, got)
			}
		})
	}
}

// fakeApplier is a hand-rolled test double for cliUtilsApplier that
// streams a scripted sequence of events on a buffered channel. The
// channel is closed once events are drained, mirroring what the real
// *apply.Applier does at end of run.
type fakeApplier struct {
	events []event.Event
	// recordedObjects captures what was passed in so tests can pin the
	// argument forwarding contract.
	recordedObjects object.UnstructuredSet
}

func (f *fakeApplier) Run(_ context.Context, _ inventory.Info, objects object.UnstructuredSet, _ apply.ApplierOptions) <-chan event.Event {
	f.recordedObjects = objects
	ch := make(chan event.Event, len(f.events))
	for _, ev := range f.events {
		ch <- ev
	}
	close(ch)
	return ch
}

// fakeDestroyer mirrors fakeApplier on the Destroy side.
type fakeDestroyer struct {
	events []event.Event
}

func (f *fakeDestroyer) Run(_ context.Context, _ inventory.Info, _ apply.DestroyerOptions) <-chan event.Event {
	ch := make(chan event.Event, len(f.events))
	for _, ev := range f.events {
		ch <- ev
	}
	close(ch)
	return ch
}

// TestRunApplyAndDrain pins the three documented outcomes: success,
// per-object failure aggregation, and ErrOperationInterrupted on
// context cancel. The success path also verifies argument forwarding
// (objects flow through to the Applier unmolested).
func TestRunApplyAndDrain(t *testing.T) {
	apiErr := errors.New("denied")
	mkObj := func(kind, name string) *unstructured.Unstructured {
		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: kind})
		obj.SetName(name)
		return obj
	}

	// dryRun: true skips the best-effort clearManagedFieldsForObjects
	// step, which would otherwise need a real KubernetesClient. The
	// contract under test (event drain -> success/error/cancel) is the
	// same in either mode.
	t.Run("success: no failures, returns nil", func(t *testing.T) {
		fa := &fakeApplier{
			events: []event.Event{
				{Type: event.ApplyType, ApplyEvent: event.ApplyEvent{Status: event.ApplySuccessful}},
			},
		}
		objs := []*unstructured.Unstructured{mkObj("Deployment", "foo")}
		a := &CLIUtilsApplier{comps: &ApplierComponents{Applier: fa}, dryRun: true}
		err := a.runApplyAndDrain(context.Background(), &SimpleInventoryInfo{namespace: "ns", name: "inv", id: "id"}, objs)
		if err != nil {
			t.Fatalf("success path: want nil err, got %v", err)
		}
		if len(fa.recordedObjects) != 1 || fa.recordedObjects[0].GetName() != "foo" {
			t.Fatalf("success path: objects not forwarded; got %v", fa.recordedObjects)
		}
	})

	t.Run("apply failure: aggregates per-object reasons", func(t *testing.T) {
		fa := &fakeApplier{
			events: []event.Event{
				{Type: event.ApplyType, ApplyEvent: event.ApplyEvent{Status: event.ApplyFailed, Error: apiErr}},
			},
		}
		a := &CLIUtilsApplier{comps: &ApplierComponents{Applier: fa}, dryRun: true}
		err := a.runApplyAndDrain(context.Background(), &SimpleInventoryInfo{}, nil)
		if err == nil {
			t.Fatal("apply failure: want non-nil error, got nil")
		}
		if !strings.Contains(err.Error(), "failed to apply resources") {
			t.Fatalf("apply failure: error should mention apply, got %q", err.Error())
		}
		if !strings.Contains(err.Error(), "denied") {
			t.Fatalf("apply failure: error should include underlying reason, got %q", err.Error())
		}
		// Ensure the sentinel is NOT used here — sentinel is for cancel only.
		if errors.Is(err, ErrOperationInterrupted) {
			t.Fatal("apply failure: must not return ErrOperationInterrupted; that sentinel is reserved for cancel")
		}
	})

	t.Run("apply skipped: surfaces as per-object failure", func(t *testing.T) {
		// cli-utils emits ApplySkipped (not ApplyFailed) when a filter
		// rejects the apply — most commonly because the resource is
		// already owned by another inventory and InventoryPolicy is
		// AdoptIfNoInventory. The drain must surface skipped resources
		// as failures so a later ApplySuccessful event in the same
		// stream cannot mask the partial actuation.
		skipErr := &inventory.PolicyPreventedActuationError{
			Strategy: actuation.ActuationStrategyApply,
			Policy:   inventory.PolicyAdoptIfNoInventory,
			Status:   inventory.NoMatch,
		}
		skipID := object.ObjMetadata{
			GroupKind: schema.GroupKind{Kind: "Namespace"},
			Name:      "shared-ns",
		}
		fa := &fakeApplier{
			events: []event.Event{
				{Type: event.ApplyType, ApplyEvent: event.ApplyEvent{Status: event.ApplySkipped, Identifier: skipID, Error: skipErr}},
				{Type: event.ApplyType, ApplyEvent: event.ApplyEvent{Status: event.ApplySuccessful}},
			},
		}
		a := &CLIUtilsApplier{comps: &ApplierComponents{Applier: fa}, dryRun: true}
		err := a.runApplyAndDrain(context.Background(), &SimpleInventoryInfo{}, nil)
		if err == nil {
			t.Fatal("apply skipped: want non-nil error, got nil")
		}
		if !strings.Contains(err.Error(), "shared-ns") {
			t.Fatalf("apply skipped: error should identify the skipped resource, got %q", err.Error())
		}
	})

	t.Run("context cancelled mid-drain returns ErrOperationInterrupted", func(t *testing.T) {
		// Use a never-closing channel so drain blocks waiting for events.
		blockingFA := &blockingApplier{ch: make(chan event.Event)}
		ctx, cancel := context.WithCancel(context.Background())
		a := &CLIUtilsApplier{comps: &ApplierComponents{Applier: blockingFA}, dryRun: true}

		errCh := make(chan error, 1)
		go func() {
			errCh <- a.runApplyAndDrain(ctx, &SimpleInventoryInfo{}, nil)
		}()
		// Give the drain a moment to begin, then cancel.
		time.Sleep(50 * time.Millisecond)
		cancel()

		select {
		case err := <-errCh:
			if !errors.Is(err, ErrOperationInterrupted) {
				t.Fatalf("cancel path: want ErrOperationInterrupted, got %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("cancel path: runApplyAndDrain did not return after context cancel")
		}
		// Drain the never-closing channel so the goroutine inside
		// drainEventChannel that may still be sending doesn't leak.
		close(blockingFA.ch)
	})
}

// blockingApplier returns the same channel on every Run call and never
// closes or sends — used to simulate a long-running apply we want to
// cancel.
type blockingApplier struct {
	ch chan event.Event
}

func (b *blockingApplier) Run(_ context.Context, _ inventory.Info, _ object.UnstructuredSet, _ apply.ApplierOptions) <-chan event.Event {
	return b.ch
}

// TestRunDestroyAndDrain mirrors TestRunApplyAndDrain on the Destroy
// side. The shapes are identical because the helpers are parallel —
// any drift between them should fail this test.
func TestRunDestroyAndDrain(t *testing.T) {
	apiErr := errors.New("permission denied")

	t.Run("success: no failures, returns nil", func(t *testing.T) {
		fd := &fakeDestroyer{
			events: []event.Event{
				{Type: event.DeleteType, DeleteEvent: event.DeleteEvent{Status: event.DeleteSuccessful}},
			},
		}
		a := &CLIUtilsApplier{comps: &ApplierComponents{Destroyer: fd}}
		err := a.runDestroyAndDrain(context.Background(), &SimpleInventoryInfo{namespace: "ns", name: "inv", id: "id"})
		if err != nil {
			t.Fatalf("success path: want nil err, got %v", err)
		}
	})

	t.Run("destroy failure: aggregates per-object reasons", func(t *testing.T) {
		fd := &fakeDestroyer{
			events: []event.Event{
				{Type: event.DeleteType, DeleteEvent: event.DeleteEvent{Status: event.DeleteFailed, Error: apiErr}},
			},
		}
		a := &CLIUtilsApplier{comps: &ApplierComponents{Destroyer: fd}}
		err := a.runDestroyAndDrain(context.Background(), &SimpleInventoryInfo{})
		if err == nil {
			t.Fatal("destroy failure: want non-nil error, got nil")
		}
		if !strings.Contains(err.Error(), "failed to destroy resources") {
			t.Fatalf("destroy failure: error should mention destroy, got %q", err.Error())
		}
		if !strings.Contains(err.Error(), "permission denied") {
			t.Fatalf("destroy failure: error should include underlying reason, got %q", err.Error())
		}
		if errors.Is(err, ErrOperationInterrupted) {
			t.Fatal("destroy failure: must not return ErrOperationInterrupted; that sentinel is reserved for cancel")
		}
	})

	t.Run("context cancelled mid-drain returns ErrOperationInterrupted", func(t *testing.T) {
		blocking := &blockingDestroyer{ch: make(chan event.Event)}
		ctx, cancel := context.WithCancel(context.Background())
		a := &CLIUtilsApplier{comps: &ApplierComponents{Destroyer: blocking}}

		errCh := make(chan error, 1)
		go func() {
			errCh <- a.runDestroyAndDrain(ctx, &SimpleInventoryInfo{})
		}()
		time.Sleep(50 * time.Millisecond)
		cancel()

		select {
		case err := <-errCh:
			if !errors.Is(err, ErrOperationInterrupted) {
				t.Fatalf("cancel path: want ErrOperationInterrupted, got %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("cancel path: runDestroyAndDrain did not return after context cancel")
		}
		close(blocking.ch)
	})
}

// blockingDestroyer mirrors blockingApplier on the Destroy side.
type blockingDestroyer struct {
	ch chan event.Event
}

func (b *blockingDestroyer) Run(_ context.Context, _ inventory.Info, _ apply.DestroyerOptions) <-chan event.Event {
	return b.ch
}

// TestResolveInventoryInfo_UsesExistingInventoryWhenValid pins the
// happy path: when setupApplierComponents loaded an inventory at
// construction (BridgeState or LiveData populated a.inventoryCM and
// a.invInfo), Apply must reuse that exact info — never re-fabricate
// from annotations, which would mint a new InventoryID and orphan the
// existing inventory ConfigMap.
func TestResolveInventoryInfo_UsesExistingInventoryWhenValid(t *testing.T) {
	existing := &SimpleInventoryInfo{
		namespace: "ns",
		name:      "inventory",
		id:        "space-uuid-unit-slug",
	}
	cm := NewInventoryConfigMap(existing)

	a := &CLIUtilsApplier{
		inventoryCM: cm,
		invInfo:     existing,
	}

	got, err := a.resolveInventoryInfo(nil)
	if err != nil {
		t.Fatalf("resolveInventoryInfo: unexpected error %v", err)
	}
	if got != existing {
		t.Fatalf("expected the existing inventory.Info to be returned unchanged; got %#v", got)
	}
}

// TestResolveInventoryInfo_FabricatesFromAnnotations pins the
// brand-new-unit path: with no inventoryCM, the helper reads the first
// object's SpaceID / UnitSlug annotations and threads them into the
// fabricated InventoryID. Operators rely on these IDs to correlate
// applies across calls; silently swapping in defaults would break that.
func TestResolveInventoryInfo_FabricatesFromAnnotations(t *testing.T) {
	obj := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata": map[string]interface{}{
			"name":      "demo",
			"namespace": "default",
			"annotations": map[string]interface{}{
				k8skit.SpaceIDAnnotation:  "abcd1234-space-uuid",
				k8skit.UnitSlugAnnotation: "billing-api",
			},
		},
	}}

	a := &CLIUtilsApplier{}
	got, err := a.resolveInventoryInfo([]*unstructured.Unstructured{obj})
	if err != nil {
		t.Fatalf("resolveInventoryInfo: unexpected error %v", err)
	}
	wantID := "abcd1234-space-uuid-billing-api"
	if string(got.GetID()) != wantID {
		t.Fatalf("inventory ID: want %q, got %q", wantID, got.GetID())
	}
}

// TestResolveInventoryInfo_FabricatesWithDefaultsWhenAnnotationsMissing:
// objects without ConfigHub annotations should fall back to the
// documented defaults rather than fail or mint a unique-per-call ID.
// This keeps Apply working on hand-authored manifests applied through
// the bridge worker's debug paths.
func TestResolveInventoryInfo_FabricatesWithDefaultsWhenAnnotationsMissing(t *testing.T) {
	obj := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata": map[string]interface{}{
			"name":      "demo",
			"namespace": "default",
		},
	}}

	a := &CLIUtilsApplier{}
	got, err := a.resolveInventoryInfo([]*unstructured.Unstructured{obj})
	if err != nil {
		t.Fatalf("resolveInventoryInfo: unexpected error %v", err)
	}
	wantID := DefaultSpaceID + "-" + DefaultUnitSlug
	if string(got.GetID()) != wantID {
		t.Fatalf("inventory ID with missing annotations: want %q (defaults), got %q",
			wantID, got.GetID())
	}
}

// TestResolveInventoryInfo_FabricatesWithDefaultsWhenNoObjects: an empty
// objects slice mustn't panic on objects[0] and must produce the same
// default-keyed Info as the missing-annotations case. This guards the
// edge case where a unit's manifests render to zero objects.
func TestResolveInventoryInfo_FabricatesWithDefaultsWhenNoObjects(t *testing.T) {
	a := &CLIUtilsApplier{}
	got, err := a.resolveInventoryInfo(nil)
	if err != nil {
		t.Fatalf("resolveInventoryInfo: unexpected error %v", err)
	}
	wantID := DefaultSpaceID + "-" + DefaultUnitSlug
	if string(got.GetID()) != wantID {
		t.Fatalf("inventory ID with no objects: want %q (defaults), got %q",
			wantID, got.GetID())
	}
}

// TestInventoryForDestroy pins the three Destroy resolution branches.
// Order matters: a valid inventoryCM wins, then a bare invInfo, then we
// error. Unlike resolveInventoryInfo (Apply), Destroy never fabricates
// from input objects — without a stored inventory we don't know what
// was previously applied, and deletion would be unsafe.
func TestInventoryForDestroy(t *testing.T) {
	existing := &SimpleInventoryInfo{
		namespace: "ns",
		name:      "inventory",
		id:        "space-uuid-unit-slug",
	}

	tcs := []struct {
		name      string
		applier   *CLIUtilsApplier
		wantInfo  inventory.Info
		wantError bool
	}{
		{
			name: "valid inventoryCM wins (BridgeState/LiveData path)",
			applier: &CLIUtilsApplier{
				inventoryCM: NewInventoryConfigMap(existing),
				invInfo:     existing,
			},
			wantInfo: existing,
		},
		{
			name: "no CM but invInfo set (degraded but recoverable)",
			applier: &CLIUtilsApplier{
				invInfo: existing,
			},
			wantInfo: existing,
		},
		{
			name:      "neither set returns error (no inventory means unsafe to destroy)",
			applier:   &CLIUtilsApplier{},
			wantError: true,
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.applier.inventoryForDestroy()
			if tc.wantError {
				if err == nil {
					t.Fatalf("inventoryForDestroy: want error, got nil (info=%v)", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("inventoryForDestroy: unexpected error %v", err)
			}
			if got != tc.wantInfo {
				t.Fatalf("inventoryForDestroy: want %#v, got %#v", tc.wantInfo, got)
			}
		})
	}
}

// TestBuildInventoryClient_Fresh covers the no-BridgeState, no-LiveData
// path: buildInventoryClient returns defaultInvInfo by reference and
// constructs the corresponding ConfigMap. SpaceID and UnitSlug are hard
// preconditions enforced by NewCLIUtilsApplier (see
// TestNewCLIUtilsApplier_RejectsMissingIdentity), so this test only
// covers the both-set case.
func TestBuildInventoryClient_Fresh(t *testing.T) {
	defaultInvInfo := &SimpleInventoryInfo{
		namespace: DefaultNamespace,
		name:      InventoryConfigMapName,
		id:        "space-abc-unit-foo",
	}
	config := ApplierConfig{SpaceID: "space-abc", UnitSlug: "unit-foo"}

	invClient, invInfo, inventoryCM := buildInventoryClient(config, defaultInvInfo)
	if invClient == nil {
		t.Fatal("buildInventoryClient: invClient must not be nil")
	}
	if inventoryCM == nil {
		t.Fatal("buildInventoryClient: inventoryCM must not be nil")
	}
	if invInfo != defaultInvInfo {
		t.Fatalf("buildInventoryClient: want defaultInvInfo by reference, got %#v", invInfo)
	}
}

// TestNewCLIUtilsApplier_RejectsMissingIdentity pins the precondition
// contract: SpaceID and UnitSlug must both be set. The applier's
// inventory identity is derived from them, and silently fabricating
// substitutes (from KubeContext or zero values) produced bugs where
// units collided on a shared ConfigMap or were keyed by a worker-local
// kubeconfig name.
func TestNewCLIUtilsApplier_RejectsMissingIdentity(t *testing.T) {
	tcs := []struct {
		name    string
		config  ApplierConfig
		wantErr string
	}{
		{
			name:    "missing SpaceID",
			config:  ApplierConfig{UnitSlug: "unit-foo"},
			wantErr: "SpaceID",
		},
		{
			name:    "missing UnitSlug",
			config:  ApplierConfig{SpaceID: "space-abc"},
			wantErr: "UnitSlug",
		},
		{
			name:    "both missing",
			config:  ApplierConfig{},
			wantErr: "SpaceID",
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewCLIUtilsApplier(tc.config)
			if err == nil {
				t.Fatalf("NewCLIUtilsApplier with %+v: want error containing %q, got nil", tc.config, tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("NewCLIUtilsApplier error: want %q in message, got %q", tc.wantErr, err.Error())
			}
		})
	}
}

// TestBuildInventoryClient_CorruptBridgeStateFallsBack pins the recovery
// path: corrupt BridgeState YAML logs and falls back to a fresh
// in-memory client + new ConfigMap, rather than propagating the parse
// error. A corrupt inventory blob shouldn't block apply/destroy on a
// unit, since the next successful apply will re-establish it.
func TestBuildInventoryClient_CorruptBridgeStateFallsBack(t *testing.T) {
	defaultInv := &SimpleInventoryInfo{
		namespace: DefaultNamespace,
		name:      InventoryConfigMapName,
		id:        "space-abc-unit-foo",
	}
	config := ApplierConfig{
		BridgeState: []byte("\x00\x01\x02 not valid yaml at all !!!"),
		SpaceID:     "space-abc",
		UnitSlug:    "unit-foo",
	}
	invClient, invInfo, inventoryCM := buildInventoryClient(config, defaultInv)
	if invClient == nil || invInfo == nil || inventoryCM == nil {
		t.Fatalf("buildInventoryClient on corrupt input must still return non-nil values; got client=%v info=%v cm=%v", invClient, invInfo, inventoryCM)
	}
	if invInfo != defaultInv {
		t.Fatalf("buildInventoryClient corrupt fallback: want defaultInv by reference, got %#v", invInfo)
	}
}

// TestRequiredCRDsForObjects locks the contract that core (no-group)
// types are skipped and that duplicates collapse to one set entry per
// GVK. Both rules matter: the apiserver's built-ins never need a CRD
// check, and a unit with N CRs of the same kind shouldn't generate N
// scans of the same group/version/kind.
func TestRequiredCRDsForObjects(t *testing.T) {
	mk := func(group, version, kind string) *unstructured.Unstructured {
		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(schema.GroupVersionKind{Group: group, Version: version, Kind: kind})
		return obj
	}
	tcs := []struct {
		name    string
		objects []*unstructured.Unstructured
		want    map[string]bool
	}{
		{
			name:    "empty input returns empty set",
			objects: nil,
			want:    map[string]bool{},
		},
		{
			name: "core types are skipped",
			objects: []*unstructured.Unstructured{
				mk("", "v1", "ConfigMap"),
				mk("", "v1", "Secret"),
			},
			want: map[string]bool{},
		},
		{
			name: "non-core types are included",
			objects: []*unstructured.Unstructured{
				mk("apps", "v1", "Deployment"),
				mk("batch", "v1", "Job"),
			},
			want: map[string]bool{
				"apps/v1/Deployment": true,
				"batch/v1/Job":       true,
			},
		},
		{
			name: "duplicates collapse to one entry",
			objects: []*unstructured.Unstructured{
				mk("apps", "v1", "Deployment"),
				mk("apps", "v1", "Deployment"),
				mk("apps", "v1", "Deployment"),
			},
			want: map[string]bool{"apps/v1/Deployment": true},
		},
		{
			name: "mixed core and non-core: only non-core",
			objects: []*unstructured.Unstructured{
				mk("", "v1", "ConfigMap"),
				mk("apps", "v1", "Deployment"),
			},
			want: map[string]bool{"apps/v1/Deployment": true},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			got := requiredCRDsForObjects(tc.objects)
			if len(got) != len(tc.want) {
				t.Fatalf("requiredCRDsForObjects: want %d entries, got %d (%v)", len(tc.want), len(got), got)
			}
			for key := range tc.want {
				if _, ok := got[key]; !ok {
					t.Fatalf("requiredCRDsForObjects: missing key %q in result %v", key, got)
				}
			}
		})
	}
}

// TestCRDCheckTimeout pins the timeout-selection contract: empty or
// unparseable input falls back to LargeWaitTimeout. A misconfigured
// timeout shouldn't block apply, so parse errors are recovered, not
// returned.
func TestCRDCheckTimeout(t *testing.T) {
	tcs := []struct {
		name        string
		waitTimeout string
		want        time.Duration
	}{
		{"empty falls back to LargeWaitTimeout", "", LargeWaitTimeout},
		{"valid duration is honoured", "30s", 30 * time.Second},
		{"valid hours", "2h", 2 * time.Hour},
		{"unparseable falls back to LargeWaitTimeout", "not-a-duration", LargeWaitTimeout},
		{"empty unit falls back", "30", LargeWaitTimeout},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			got := crdCheckTimeout(tc.waitTimeout)
			if got != tc.want {
				t.Fatalf("crdCheckTimeout(%q): want %s, got %s", tc.waitTimeout, tc.want, got)
			}
		})
	}
}

// TestCRDBackoffDelay pins the documented backoff schedule: 2s on the
// first attempt, exponential through retry 4, then capped at 30s.
// Drift here would either hammer the apiserver or stall installs, so
// the values are part of the contract.
func TestCRDBackoffDelay(t *testing.T) {
	tcs := []struct {
		retryCount int
		want       time.Duration
	}{
		{0, 2 * time.Second},
		{1, 2 * time.Second},
		{2, 4 * time.Second},
		{3, 8 * time.Second},
		{4, 16 * time.Second},
		{5, 30 * time.Second},
		{6, 30 * time.Second},
		{100, 30 * time.Second},
	}
	for _, tc := range tcs {
		t.Run(tc.want.String(), func(t *testing.T) {
			got := crdBackoffDelay(tc.retryCount)
			if got != tc.want {
				t.Fatalf("crdBackoffDelay(%d): want %s, got %s", tc.retryCount, tc.want, got)
			}
		})
	}
}

// TestGVKRegistered locks the lookup contract: exact group + version
// match required, kind match is case-sensitive, malformed entries in
// the discovery list are skipped (not fatal). The case sensitivity
// matters — "Deployment" vs "deployment" reflects a real CRD bug we
// don't want to paper over by lower-casing.
func TestGVKRegistered(t *testing.T) {
	apiResourceList := []*metav1.APIResourceList{
		{
			GroupVersion: "apps/v1",
			APIResources: []metav1.APIResource{{Kind: "Deployment"}, {Kind: "StatefulSet"}},
		},
		{
			GroupVersion: "example.com/v1alpha1",
			APIResources: []metav1.APIResource{{Kind: "Widget"}},
		},
		{
			GroupVersion: "malformed//",
			APIResources: []metav1.APIResource{{Kind: "Garbage"}},
		},
	}
	tcs := []struct {
		name string
		gvk  schema.GroupVersionKind
		want bool
	}{
		{"registered apps/v1 Deployment", schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"}, true},
		{"registered apps/v1 StatefulSet", schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "StatefulSet"}, true},
		{"registered CRD-style", schema.GroupVersionKind{Group: "example.com", Version: "v1alpha1", Kind: "Widget"}, true},
		{"wrong version", schema.GroupVersionKind{Group: "apps", Version: "v1beta1", Kind: "Deployment"}, false},
		{"wrong group", schema.GroupVersionKind{Group: "extensions", Version: "v1", Kind: "Deployment"}, false},
		{"unknown kind in registered group", schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Frobnicator"}, false},
		{"case-sensitive kind", schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "deployment"}, false},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			got := gvkRegistered(apiResourceList, tc.gvk)
			if got != tc.want {
				t.Fatalf("gvkRegistered(%v): want %v, got %v", tc.gvk, tc.want, got)
			}
		})
	}
}

// TestDestroyEventFailure pins the destroy variant: ErrorType and
// DeleteFailed contribute; ApplyFailed during destroy is logged but
// not terminal (destroy can complete even if some inventory updates
// fail).
func TestDestroyEventFailure(t *testing.T) {
	apiErr := errors.New("permission denied")
	tcs := []struct {
		name string
		ev   event.Event
		want string
	}{
		{"ErrorType returns the error string", event.Event{Type: event.ErrorType, ErrorEvent: event.ErrorEvent{Err: apiErr}}, "permission denied"},
		{"DeleteFailed with error contributes a failure", event.Event{Type: event.DeleteType, DeleteEvent: event.DeleteEvent{Status: event.DeleteFailed, Error: apiErr}}, ": permission denied"},
		{"DeleteSuccessful is silent", event.Event{Type: event.DeleteType, DeleteEvent: event.DeleteEvent{Status: event.DeleteSuccessful}}, ""},
		{"ApplyFailed during destroy is logged, not terminal", event.Event{Type: event.ApplyType, ApplyEvent: event.ApplyEvent{Status: event.ApplyFailed, Error: apiErr}}, ""},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			got := destroyEventFailure(tc.ev)
			if !strings.Contains(got, tc.want) {
				t.Fatalf("destroyEventFailure: want substring %q, got %q", tc.want, got)
			}
		})
	}
}

// TestParseInventoryID pins the "{spaceID}-{unitSlug}" decoding.
// Inputs that don't fit the shape return ("", input) so callers can
// fall back to printing the raw ID.
func TestParseInventoryID(t *testing.T) {
	const uuid = "00000000-0000-0000-0000-000000000000"
	tcs := []struct {
		name      string
		invID     string
		wantSpace string
		wantSlug  string
	}{
		{"default UUID and slug", uuid + "-default", uuid, "default"},
		{"slug containing hyphens preserved verbatim", uuid + "-my-app-prod", uuid, "my-app-prod"},
		{"too short to be UUID-prefixed", "short-name", "", "short-name"},
		{"missing hyphen at UUID boundary", uuid + "default", "", uuid + "default"},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			gotSpace, gotSlug := parseInventoryID(tc.invID)
			if gotSpace != tc.wantSpace || gotSlug != tc.wantSlug {
				t.Fatalf("parseInventoryID(%q): want (%q,%q), got (%q,%q)",
					tc.invID, tc.wantSpace, tc.wantSlug, gotSpace, gotSlug)
			}
		})
	}
}

// TestFormatInventoryConflicts pins the operator-facing message: empty
// returns nil, populated returns a sorted, structured error that names
// the owning unit (or falls back to the raw inventory ID when the ID
// can't be decoded).
func TestFormatInventoryConflicts(t *testing.T) {
	const spaceA = "00000000-0000-0000-0000-000000000000"

	t.Run("empty returns nil so callers can use it directly", func(t *testing.T) {
		if err := formatInventoryConflicts(nil); err != nil {
			t.Fatalf("formatInventoryConflicts(nil): want nil, got %v", err)
		}
	})

	t.Run("decoded owner renders unit and space", func(t *testing.T) {
		err := formatInventoryConflicts([]inventoryConflict{{
			Identifier:    object.ObjMetadata{GroupKind: schema.GroupKind{Kind: "Namespace"}, Name: "shared-ns"},
			OwnerInvID:    spaceA + "-other-unit",
			OwnerSpaceID:  spaceA,
			OwnerUnitSlug: "other-unit",
		}})
		if err == nil {
			t.Fatal("want error, got nil")
		}
		want := []string{"shared-ns", `"other-unit"`, spaceA, "1 resource(s)"}
		for _, s := range want {
			if !strings.Contains(err.Error(), s) {
				t.Errorf("error %q missing %q", err.Error(), s)
			}
		}
	})

	t.Run("undecodable owner falls back to raw inventory ID", func(t *testing.T) {
		err := formatInventoryConflicts([]inventoryConflict{{
			Identifier: object.ObjMetadata{GroupKind: schema.GroupKind{Kind: "ConfigMap"}, Namespace: "ns", Name: "cm"},
			OwnerInvID: "raw-opaque-id",
		}})
		if err == nil || !strings.Contains(err.Error(), "raw-opaque-id") {
			t.Fatalf("want error mentioning raw ID, got %v", err)
		}
	})

	t.Run("multiple conflicts are sorted by identifier for stable output", func(t *testing.T) {
		err := formatInventoryConflicts([]inventoryConflict{
			{Identifier: object.ObjMetadata{GroupKind: schema.GroupKind{Kind: "Namespace"}, Name: "zeta"}, OwnerInvID: "z"},
			{Identifier: object.ObjMetadata{GroupKind: schema.GroupKind{Kind: "Namespace"}, Name: "alpha"}, OwnerInvID: "a"},
		})
		if err == nil {
			t.Fatal("want error, got nil")
		}
		got := err.Error()
		if strings.Index(got, "alpha") > strings.Index(got, "zeta") {
			t.Fatalf("want alpha before zeta in stable-sorted message, got %q", got)
		}
	})
}

// stubKubeClient satisfies KubernetesClient by embedding the interface
// (nil for unused methods) and intercepting only Get. The objects map
// keys on "namespace/name" and supplies the annotations the stub
// returns. The optional labels map (same keying) supplies labels for
// tests that exercise label-based logic. Missing keys yield a NotFound
// error.
type stubKubeClient struct {
	KubernetesClient
	objects map[string]map[string]string
	labels  map[string]map[string]string
	getErr  error // when non-nil, Get returns this for any key (mismatch test)
}

func (s *stubKubeClient) Get(_ context.Context, key client.ObjectKey, obj client.Object, _ ...client.GetOption) error {
	if s.getErr != nil {
		return s.getErr
	}
	ann, ok := s.objects[key.Namespace+"/"+key.Name]
	if !ok {
		return apierrors.NewNotFound(schema.GroupResource{Resource: "objects"}, key.Name)
	}
	if u, isU := obj.(*unstructured.Unstructured); isU {
		u.SetName(key.Name)
		u.SetNamespace(key.Namespace)
		u.SetAnnotations(ann)
		if lbl, ok := s.labels[key.Namespace+"/"+key.Name]; ok {
			u.SetLabels(lbl)
		}
	}
	return nil
}

// TestPartitionByInventoryOwnership pins the pre-flight contract.
// Objects are sorted into three buckets:
//   - toApply: absent, unowned, or already ours
//   - conflicts: owned by another inventory and not shared infra
//   - excluded (neither toApply nor conflicts): shared bridge infra
//     that matches on app.kubernetes.io/managed-by labels — dropped
//     from the apply set so cli-utils' own InventoryPolicyApplyFilter
//     does not skip it mid-apply and trip the drain.
//
// NotFound is benign (first apply); other Get errors abort the scan
// rather than silently apply on a partial cluster view.
func TestPartitionByInventoryOwnership(t *testing.T) {
	const ownID = "00000000-0000-0000-0000-000000000000-mine"
	const otherID = "11111111-1111-1111-1111-111111111111-other"

	mkObj := func(kind, namespace, name string) *unstructured.Unstructured {
		o := &unstructured.Unstructured{}
		o.SetGroupVersionKind(schema.GroupVersionKind{Version: "v1", Kind: kind})
		o.SetNamespace(namespace)
		o.SetName(name)
		return o
	}

	t.Run("absent and matching objects pass through to toApply", func(t *testing.T) {
		stub := &stubKubeClient{objects: map[string]map[string]string{
			"/already-mine":     {inventory.OwningInventoryKey: ownID},
			"default/no-anno":   nil,
			"default/empty-own": {inventory.OwningInventoryKey: ""},
		}}
		a := &CLIUtilsApplier{comps: &ApplierComponents{KubernetesClient: stub}}
		objects := []*unstructured.Unstructured{
			mkObj("Namespace", "", "absent"),
			mkObj("Namespace", "", "already-mine"),
			mkObj("ConfigMap", "default", "no-anno"),
			mkObj("ConfigMap", "default", "empty-own"),
		}
		toApply, conflicts, err := a.partitionByInventoryOwnership(context.Background(), ownID, objects)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(conflicts) != 0 {
			t.Fatalf("want no conflicts, got %+v", conflicts)
		}
		if len(toApply) != 4 {
			t.Fatalf("want all 4 objects in toApply, got %d", len(toApply))
		}
	})

	t.Run("foreign owner is recorded with parsed identity and dropped from toApply", func(t *testing.T) {
		stub := &stubKubeClient{objects: map[string]map[string]string{
			"/shared-ns": {inventory.OwningInventoryKey: otherID},
		}}
		a := &CLIUtilsApplier{comps: &ApplierComponents{KubernetesClient: stub}}
		toApply, conflicts, err := a.partitionByInventoryOwnership(context.Background(), ownID,
			[]*unstructured.Unstructured{mkObj("Namespace", "", "shared-ns")})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(conflicts) != 1 {
			t.Fatalf("want 1 conflict, got %d", len(conflicts))
		}
		if conflicts[0].OwnerUnitSlug != "other" || conflicts[0].OwnerSpaceID != "11111111-1111-1111-1111-111111111111" {
			t.Errorf("owner identity not parsed: got %+v", conflicts[0])
		}
		if conflicts[0].Identifier.Name != "shared-ns" {
			t.Errorf("identifier mismatch: got %+v", conflicts[0].Identifier)
		}
		if len(toApply) != 0 {
			t.Errorf("conflicted object must not be in toApply, got %d", len(toApply))
		}
	})

	t.Run("non-NotFound Get error aborts the scan", func(t *testing.T) {
		stub := &stubKubeClient{getErr: errors.New("api server unreachable")}
		a := &CLIUtilsApplier{comps: &ApplierComponents{KubernetesClient: stub}}
		_, _, err := a.partitionByInventoryOwnership(context.Background(), ownID,
			[]*unstructured.Unstructured{mkObj("Namespace", "", "any")})
		if err == nil {
			t.Fatal("want error, got nil")
		}
		if !strings.Contains(err.Error(), "inventory pre-flight") {
			t.Errorf("error should be tagged as pre-flight, got %q", err.Error())
		}
	})

	t.Run("shared bridge infrastructure is excluded from both toApply and conflicts", func(t *testing.T) {
		// The argocd-oci bridge generates a deterministically named
		// repo-creds Secret per OCI host and includes it in every
		// unit's apply payload. The second unit's apply must not pass
		// the Secret to cli-utils (which would skip it under
		// PolicyAdoptIfNoInventory) and must not flag it as a
		// conflict. The shared marker is matching
		// app.kubernetes.io/managed-by labels on both sides.
		stub := &stubKubeClient{
			objects: map[string]map[string]string{
				"argocd/confighub-oci-creds-host": {inventory.OwningInventoryKey: otherID},
			},
			labels: map[string]map[string]string{
				"argocd/confighub-oci-creds-host": {k8skit.LabelManagedBy: "argocd-oci-bridge"},
			},
		}
		a := &CLIUtilsApplier{comps: &ApplierComponents{KubernetesClient: stub}}
		desired := mkObj("Secret", "argocd", "confighub-oci-creds-host")
		desired.SetLabels(map[string]string{k8skit.LabelManagedBy: "argocd-oci-bridge"})
		toApply, conflicts, err := a.partitionByInventoryOwnership(context.Background(), ownID,
			[]*unstructured.Unstructured{desired})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(conflicts) != 0 {
			t.Fatalf("want 0 conflicts (shared infrastructure exempt), got %+v", conflicts)
		}
		if len(toApply) != 0 {
			t.Fatalf("shared infrastructure must be excluded from toApply too, got %d", len(toApply))
		}
	})

	t.Run("mismatched managed-by still surfaces as conflict", func(t *testing.T) {
		// If the live object's managed-by points at a different
		// manager (e.g. another tool claimed the same name first),
		// co-management is NOT safe and the conflict must surface.
		stub := &stubKubeClient{
			objects: map[string]map[string]string{
				"argocd/shared": {inventory.OwningInventoryKey: otherID},
			},
			labels: map[string]map[string]string{
				"argocd/shared": {k8skit.LabelManagedBy: "some-other-tool"},
			},
		}
		a := &CLIUtilsApplier{comps: &ApplierComponents{KubernetesClient: stub}}
		desired := mkObj("Secret", "argocd", "shared")
		desired.SetLabels(map[string]string{k8skit.LabelManagedBy: "argocd-oci-bridge"})
		_, conflicts, err := a.partitionByInventoryOwnership(context.Background(), ownID,
			[]*unstructured.Unstructured{desired})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(conflicts) != 1 {
			t.Fatalf("want 1 conflict (managed-by mismatch), got %d", len(conflicts))
		}
	})

	t.Run("empty managed-by on either side does not exempt", func(t *testing.T) {
		// User-authored resources without the label are still
		// strictly checked. An empty value on either side is never a
		// match.
		stub := &stubKubeClient{
			objects: map[string]map[string]string{
				"default/cm": {inventory.OwningInventoryKey: otherID},
			},
			labels: map[string]map[string]string{
				"default/cm": {k8skit.LabelManagedBy: ""},
			},
		}
		a := &CLIUtilsApplier{comps: &ApplierComponents{KubernetesClient: stub}}
		desired := mkObj("ConfigMap", "default", "cm")
		desired.SetLabels(map[string]string{k8skit.LabelManagedBy: "argocd-oci-bridge"})
		_, conflicts, err := a.partitionByInventoryOwnership(context.Background(), ownID,
			[]*unstructured.Unstructured{desired})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(conflicts) != 1 {
			t.Fatalf("want 1 conflict (empty live managed-by), got %d", len(conflicts))
		}
	})
}
