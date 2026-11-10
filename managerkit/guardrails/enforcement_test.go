// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package guardrails

import (
	"reflect"
	"testing"
	"time"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
)

func TestStage(t *testing.T) {
	for _, tc := range []struct {
		name           string
		disabled, warn bool
		selected       bool
		want           string
	}{
		{"not selected", false, true, false, StageCreated},
		{"selected, warns", false, true, true, StageAudited},
		{"selected, blocks", false, false, true, StageEnforced},
		{"disabled wins over selection", true, false, true, StageDisabled},
		{"disabled and not selected", true, true, false, StageDisabled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := Stage(&goclientnew.Trigger{Disabled: tc.disabled, Warn: tc.warn}, tc.selected)
			if got != tc.want {
				t.Errorf("Stage = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestUnitVerdicts(t *testing.T) {
	renamed, passed, warned := uuid.New(), uuid.New(), uuid.New()
	u := &goclientnew.Unit{
		ValidationTriggerIDs: map[string]goclientnew.UUID{
			// A renamed Trigger passed under its old name and fails under its new one.
			"policy/old-name/vet-cel": renamed,
			"policy/new-name/vet-cel": renamed,
			"policy/passes/vet-cel":   passed,
			"policy/warns/vet-cel":    warned,
		},
		ValidationErrors:   map[string]bool{"policy/new-name/vet-cel": true},
		ValidationWarnings: map[string]bool{"policy/warns/vet-cel": true},
	}
	got := unitVerdicts(u)
	want := map[goclientnew.UUID]verdict{renamed: verdictError, passed: verdictPassed, warned: verdictWarning}
	if len(got) != len(want) {
		t.Fatalf("got %d verdicts, want %d: %v", len(got), len(want), got)
	}
	for id, v := range want {
		if got[id] != v {
			t.Errorf("verdict of %s = %d, want %d", id, got[id], v)
		}
	}
}

func TestPlanEnforcementChange(t *testing.T) {
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	str := func(s string) *string { return &s }
	yes, no := true, false
	for _, tc := range []struct {
		name        string
		trigger     goclientnew.Trigger
		update      EnforcementUpdate
		wantChanged bool
		wantPatch   map[string]any
		wantFields  []string
	}{
		{
			name:        "sets priority and due date",
			trigger:     goclientnew.Trigger{Warn: true},
			update:      EnforcementUpdate{Priority: str("High"), EnforcedBy: str("2026-12-31T00:00:00Z")},
			wantChanged: true,
			wantPatch: map[string]any{"Annotations": map[string]any{
				"Priority": "High", "EnforcedBy": "2026-12-31T00:00:00Z",
			}},
			wantFields: []string{"Priority: (none) -> High", "EnforcedBy: (none) -> 2026-12-31T00:00:00Z"},
		},
		{
			name:        "removes the priority",
			trigger:     goclientnew.Trigger{Annotations: map[string]string{"Priority": "Low"}},
			update:      EnforcementUpdate{Priority: str("")},
			wantChanged: true,
			wantPatch:   map[string]any{"Annotations": map[string]any{"Priority": nil}},
			wantFields:  []string{"Priority: Low -> (none)"},
		},
		{
			name:        "enforcing a warning rule records when",
			trigger:     goclientnew.Trigger{Warn: true},
			update:      EnforcementUpdate{Enforce: &yes},
			wantChanged: true,
			wantPatch: map[string]any{
				"Warn":        false,
				"Annotations": map[string]any{"EnforcedAt": "2026-11-09T12:00:00Z"},
			},
			wantFields: []string{"Warn: true -> false", "EnforcedAt: (none) -> 2026-11-09T12:00:00Z"},
		},
		{
			name:        "enforcing a rule already enforced changes nothing",
			trigger:     goclientnew.Trigger{Warn: false, Annotations: map[string]string{"EnforcedAt": "2026-10-01T00:00:00Z"}},
			update:      EnforcementUpdate{Enforce: &yes},
			wantChanged: false,
		},
		{
			name:        "returning to audit removes when it was enforced",
			trigger:     goclientnew.Trigger{Warn: false, Annotations: map[string]string{"EnforcedAt": "2026-10-01T00:00:00Z"}},
			update:      EnforcementUpdate{Enforce: &no},
			wantChanged: true,
			wantPatch: map[string]any{
				"Warn":        true,
				"Annotations": map[string]any{"EnforcedAt": nil},
			},
			wantFields: []string{"Warn: false -> true", "EnforcedAt: 2026-10-01T00:00:00Z -> (none)"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, changed := planEnforcementChange("rule", &tc.trigger, tc.update, now)
			if changed != tc.wantChanged {
				t.Fatalf("changed = %t, want %t (fields %v)", changed, tc.wantChanged, c.Fields)
			}
			if !changed {
				return
			}
			if !reflect.DeepEqual(c.patch, tc.wantPatch) {
				t.Errorf("patch = %#v, want %#v", c.patch, tc.wantPatch)
			}
			if !reflect.DeepEqual(c.Fields, tc.wantFields) {
				t.Errorf("fields = %q, want %q", c.Fields, tc.wantFields)
			}
		})
	}
}
