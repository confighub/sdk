// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package cubapi

import (
	"testing"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

func TestOperationClearance(t *testing.T) {
	tests := []struct {
		name  string
		specs []string
		merge bool
		want  string
	}{
		{name: "not a merge, none given", want: "Override"},
		{name: "a merge, none given", merge: true, want: ""},
		// A clearance given replaces the default rather than adding to it.
		{name: "not a merge, one given", specs: []string{"owner=platform"}, want: "owner=platform"},
		{name: "a merge, one given", specs: []string{"Override=Upstream"}, merge: true, want: "Override=Upstream"},
		// An empty one is how a change is cleared for nothing.
		{name: "not a merge, empty given", specs: []string{""}, want: ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			clearance, err := OperationClearance(test.specs, test.merge)
			if err != nil {
				t.Fatal(err)
			}
			if got := FormatClearance(&clearance); got != test.want {
				t.Errorf("got %q, want %q", got, test.want)
			}
		})
	}
	if _, err := OperationClearance([]string{"owner=a,b"}, false); err == nil {
		t.Error("a malformed clearance was accepted")
	}
}

// Each call returns its own Clearance, so that extending one does not change the default.
func TestDefaultClearanceIsFresh(t *testing.T) {
	first := DefaultClearance()
	first[0].Key = "changed"
	if got := DefaultClearance()[0].Key; got != "Override" {
		t.Errorf("DefaultClearance()[0].Key = %q after a caller changed its copy", got)
	}
}

func TestClearanceToRetry(t *testing.T) {
	conflicts := []goclientnew.MutationConflict{
		{Reason: "Guarded", Guards: []goclientnew.WithheldGuard{{Key: "owner", Value: "platform"}}},
		{Reason: "Subtracted"},
	}
	retry := ClearanceToRetry(conflicts, goclientnew.Clearance{{Key: "Scale", Operator: "Exists"}})
	if got := FormatClearance(&retry); got != "owner=platform" {
		t.Errorf("sent without the default: %q", got)
	}
	retry = ClearanceToRetry(conflicts, DefaultClearance())
	if got := FormatClearance(&retry); got != "Override owner=platform" {
		t.Errorf("sent with the default: %q", got)
	}
	// Nothing withheld by a guard needs no clearance, the default included.
	unguarded := ClearanceToRetry([]goclientnew.MutationConflict{{Reason: "Subtracted"}}, DefaultClearance())
	if len(unguarded) != 0 {
		t.Errorf("for no guarded conflict: %q", FormatClearance(&unguarded))
	}
}

func TestMergeClearances(t *testing.T) {
	first := goclientnew.Clearance{{Key: "Override", Operator: "Exists"}}
	second := goclientnew.Clearance{{Key: "Override", Operator: "Exists"}, {Key: "owner", Operator: "In", Values: []string{"a"}}}
	merged := MergeClearances(first, second)
	if got := FormatClearance(&merged); got != "Override owner=a" {
		t.Errorf("MergeClearances = %q", got)
	}
	if len(first) != 1 {
		t.Error("MergeClearances changed its first argument")
	}
}
