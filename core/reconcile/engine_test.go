// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package reconcile

import (
	"bytes"
	"context"
	"io"
	"slices"
	"strings"
	"testing"
)

// fakeRunner is a scriptable CubRunner. respond returns the stdout a given
// cub invocation should produce (and an optional error); nil respond makes
// every call a silent success. All calls are recorded in calls.
type fakeRunner struct {
	calls   [][]string
	respond func(args []string) (string, error)
}

func (f *fakeRunner) Run(_ context.Context, args []string, stdout, stderr io.Writer) error {
	f.calls = append(f.calls, args)
	if f.respond == nil {
		return nil
	}
	out, err := f.respond(args)
	if out != "" && stdout != nil {
		io.WriteString(stdout, out)
	}
	return err
}

// hasArg reports whether args contains all of subs in order-independent form.
func hasAll(args []string, subs ...string) bool {
	set := map[string]struct{}{}
	for _, a := range args {
		set[a] = struct{}{}
	}
	for _, s := range subs {
		if _, ok := set[s]; !ok {
			return false
		}
	}
	return true
}

func TestComputeBuckets(t *testing.T) {
	// Live Space owns: unitB, unitC (placeholder/bodyless), unitD, and a
	// bookkeeping record slug that is Ignored. Desired: unitA (new → add),
	// unitB (changed → update), unitC (bodyless → neither). unitD drops out
	// of desired → delete. The record is Ignored → never a delete.
	runner := &fakeRunner{
		respond: func(args []string) (string, error) {
			if hasAll(args, "unit", "list") {
				return "unitB\nunitC\nunitD\nrecord\n", nil
			}
			if hasAll(args, "--dry-run") {
				// A real change only for unitB.
				if slices.Contains(args, "unitB") {
					return "New changes from update from x:\nResource: apps/v1/Deployment d/x\n  ~ [Update] spec.replicas  (#1)\n    1 →     2\n", nil
				}
				return "No new changes", nil
			}
			return "", nil
		},
	}
	e := New(runner)
	src := SpaceSource{
		Space:     "web-base",
		ListWhere: "Labels.Package='web'",
		Ignore:    []string{"record"},
		Desired: []DesiredUnit{
			{Slug: "unitA", Path: "/tmp/a.yaml"},
			{Slug: "unitB", Content: []byte("kind: Deployment\n")},
			{Slug: "unitC", Bodyless: true},
		},
	}
	plan, err := e.Compute(context.Background(), []SpaceSource{src})
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	if len(plan.Spaces) != 1 {
		t.Fatalf("want 1 space, got %d", len(plan.Spaces))
	}
	sp := plan.Spaces[0]
	if got := slugsOf(sp.Adds); !equalStrings(got, []string{"unitA"}) {
		t.Errorf("adds = %v, want [unitA]", got)
	}
	if got := slugsOf(sp.Updates); !equalStrings(got, []string{"unitB"}) {
		t.Errorf("updates = %v, want [unitB]", got)
	}
	if got := slugsOf(sp.Deletes); !equalStrings(got, []string{"unitD"}) {
		t.Errorf("deletes = %v, want [unitD] (record must be Ignored)", got)
	}
	// The update carried the filtered diff text forward.
	if !strings.Contains(sp.Updates[0].DiffText, "spec.replicas") {
		t.Errorf("update diff text missing: %q", sp.Updates[0].DiffText)
	}
}

func TestApplyNoChangesNoOp(t *testing.T) {
	// Apply on a plan with no changes must not invoke cub at all.
	runner := &fakeRunner{respond: func(args []string) (string, error) {
		t.Errorf("cub must not be called on a no-op plan, got: %v", args)
		return "", nil
	}}
	var stdout, stderr bytes.Buffer
	res, err := New(runner).Apply(context.Background(),
		Plan{Spaces: []SpacePlan{{Space: "x"}}},
		ApplyOptions{Stdout: &stdout, Stderr: &stderr})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if res.Created != 0 || res.Updated != 0 || res.Deleted != 0 {
		t.Errorf("expected zero counts, got %+v", res)
	}
	if len(runner.calls) != 0 {
		t.Errorf("expected no cub calls, got %v", runner.calls)
	}
	if stdout.Len() != 0 || stderr.Len() != 0 {
		t.Errorf("expected no output, got stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestEmptyUnitsRefusesWithoutYes(t *testing.T) {
	// Without Yes, emptying must refuse the whole batch up front — before any
	// cub call — listing every Unit it would empty.
	runner := &fakeRunner{respond: func(args []string) (string, error) {
		t.Errorf("cub must not be called when refusing, got: %v", args)
		return "", nil
	}}
	var stdout, stderr bytes.Buffer
	n, err := New(runner).emptyUnits(context.Background(), "statusboard-prod",
		[]SlugDiff{{Slug: "deployment-gone"}, {Slug: "service-gone"}},
		ApplyOptions{Yes: false}, &stdout, &stderr)
	if err == nil {
		t.Fatal("expected error when Yes is not set")
	}
	if n != 0 {
		t.Errorf("expected 0 emptied, got %d", n)
	}
	out := stdout.String()
	for _, slug := range []string{"deployment-gone", "service-gone"} {
		if !strings.Contains(out, slug) {
			t.Errorf("expected refusal to list %q, got:\n%s", slug, out)
		}
	}
	if strings.Contains(out, "Emptied") {
		t.Errorf("must not empty anything when refusing, got:\n%s", out)
	}
}

func TestDescriptionOrDefault(t *testing.T) {
	cases := []struct {
		d    string
		sp   SpacePlan
		want string
	}{
		{"", SpacePlan{DisplayName: "hello", DisplayVersion: "0.1.0"}, "reconcile from hello@0.1.0"},
		{"", SpacePlan{DisplayName: "hello"}, "reconcile from hello"},
		{"", SpacePlan{}, "reconcile update"},
		{"manual desc", SpacePlan{DisplayName: "hello"}, "manual desc"},
	}
	for _, tc := range cases {
		if got := descriptionOrDefault(tc.d, tc.sp); got != tc.want {
			t.Errorf("descriptionOrDefault(%q, %+v) = %q, want %q", tc.d, tc.sp, got, tc.want)
		}
	}
}

func TestPlanCounts(t *testing.T) {
	p := Plan{
		Spaces: []SpacePlan{
			{Adds: []SlugDiff{{Slug: "a"}, {Slug: "b"}}, Updates: []SlugDiff{{Slug: "c"}}},
			{Deletes: []SlugDiff{{Slug: "d"}}},
		},
	}
	if !p.HasChanges() {
		t.Errorf("HasChanges should be true")
	}
	a, u, d := p.Counts()
	if a != 2 || u != 1 || d != 1 {
		t.Errorf("counts = (%d, %d, %d), want (2, 1, 1)", a, u, d)
	}
	var empty Plan
	if empty.HasChanges() {
		t.Errorf("empty plan should have no changes")
	}
}

// --- test helpers ---

func slugsOf(ds []SlugDiff) []string {
	out := make([]string, 0, len(ds))
	for _, d := range ds {
		out = append(out, d.Slug)
	}
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
