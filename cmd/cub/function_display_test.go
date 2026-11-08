// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"strings"
	"testing"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

// A Unit whose functions ran but whose result was not saved comes back with an Error, and from
// some servers with Success set as well. Either way the invocation failed there: the response
// says so instead of "Config data changed", and the command exits non-zero.
func TestFailedInvocationIsReported(t *testing.T) {
	ok := goclientnew.FunctionInvocationsResponse{UnitSlug: "web", Success: true, Mutators: []int{0}}
	notSaved := goclientnew.FunctionInvocationsResponse{UnitSlug: "api", Success: true, Mutators: []int{0},
		Error: &goclientnew.ResponseError{Message: "Failed to update unit: unit api changed while the functions ran"}}
	failed := goclientnew.FunctionInvocationsResponse{UnitSlug: "db",
		Error: &goclientnew.ResponseError{Message: "function failed"}}

	if err := failedInvocationsError(&[]goclientnew.FunctionInvocationsResponse{ok}); err != nil {
		t.Errorf("no Unit failed, got %v", err)
	}
	err := failedInvocationsError(&[]goclientnew.FunctionInvocationsResponse{ok, notSaved, failed})
	if err == nil || !strings.Contains(err.Error(), "2 of 3") {
		t.Errorf("want the invocation to fail on 2 of 3 units, got %v", err)
	}

	out := captureStdout(t, func() {
		outputFunctionInvocationResponse(&[]goclientnew.FunctionInvocationsResponse{notSaved})
	})
	if !strings.Contains(out, "Function(s) failed on unit") || !strings.Contains(out, "changed while the functions ran") {
		t.Errorf("want the failure and its reason, got:\n%s", out)
	}
	if strings.Contains(out, "Config data changed") {
		t.Errorf("a result that was not saved changed nothing, got:\n%s", out)
	}
}

// What the summary says about the data follows what the write did, not what the functions
// proposed: guards that withheld every change leave the data as it was, and a dry run only
// says what would happen.
func TestInvocationSummaryFollowsTheWrite(t *testing.T) {
	withheld := goclientnew.MutationConflictList{{Reason: "Guarded", Path: "spec.replicas"}}
	changed := goclientnew.FunctionInvocationsResponse{UnitSlug: "web", Success: true, Mutators: []int{0},
		HasNewMutations: true}
	partlyWithheld := changed
	partlyWithheld.Conflicts = &withheld
	allWithheld := goclientnew.FunctionInvocationsResponse{UnitSlug: "web", Success: true, Mutators: []int{0},
		Conflicts: &withheld}
	unchanged := goclientnew.FunctionInvocationsResponse{UnitSlug: "web", Success: true}

	tests := []struct {
		name   string
		resp   goclientnew.FunctionInvocationsResponse
		dryRun bool
		want   string
	}{
		{"changed", changed, false, "Config data changed"},
		{"partly withheld", partlyWithheld, false, "Config data changed"},
		{"withheld entirely", allWithheld, false, "Config data not changed"},
		{"unchanged", unchanged, false, "Config data not changed"},
		{"dry run", changed, true, "Config data would change"},
		{"dry run withheld entirely", allWithheld, true, "Config data would not change"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			savedDryRun := dryRun
			t.Cleanup(func() { dryRun = savedDryRun })
			dryRun = tc.dryRun

			out := captureStdout(t, func() {
				outputFunctionInvocationResponse(&[]goclientnew.FunctionInvocationsResponse{tc.resp})
			})
			lines := strings.Split(out, "\n")
			if len(lines) < 2 || lines[1] != tc.want {
				t.Errorf("want %q after the status line, got:\n%s", tc.want, out)
			}
		})
	}
}

// Nothing was written to wait on after a dry run, a write the guards withheld entirely, or a
// failed invocation, so none of them says it is awaiting triggers or reaches the server, which
// this test has none of.
func TestAwaitInvocationTriggersSkipsUnwrittenUnits(t *testing.T) {
	withheld := goclientnew.MutationConflictList{{Reason: "Guarded", Path: "spec.replicas"}}
	changed := goclientnew.FunctionInvocationsResponse{UnitSlug: "web", Success: true, Mutators: []int{0},
		HasNewMutations: true}
	allWithheld := goclientnew.FunctionInvocationsResponse{UnitSlug: "web", Success: true, Mutators: []int{0},
		Conflicts: &withheld}
	unchanged := goclientnew.FunctionInvocationsResponse{UnitSlug: "web", Success: true}
	failed := changed
	failed.Error = &goclientnew.ResponseError{Message: "function failed"}

	tests := []struct {
		name   string
		resps  []goclientnew.FunctionInvocationsResponse
		dryRun bool
	}{
		{"dry run", []goclientnew.FunctionInvocationsResponse{changed}, true},
		{"withheld entirely", []goclientnew.FunctionInvocationsResponse{allWithheld}, false},
		{"unchanged", []goclientnew.FunctionInvocationsResponse{unchanged}, false},
		{"failed", []goclientnew.FunctionInvocationsResponse{failed}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			savedDryRun, savedWait := dryRun, wait
			t.Cleanup(func() { dryRun, wait = savedDryRun, savedWait })
			dryRun, wait = tc.dryRun, true

			var err error
			out := captureStdout(t, func() { err = awaitInvocationTriggers(&tc.resps) })
			if err != nil || out != "" {
				t.Errorf("want nothing awaited, got err %v and output %q", err, out)
			}
		})
	}
}
