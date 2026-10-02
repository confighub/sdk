// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"testing"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

func setDisplayNameAndHiddenReasonFlags(t *testing.T, displayName, hiddenReason string) {
	t.Helper()
	previousDisplayName, previousHiddenReason := displayNameFlag, hiddenReasonFlag
	displayNameFlag, hiddenReasonFlag = displayName, hiddenReason
	t.Cleanup(func() { displayNameFlag, hiddenReasonFlag = previousDisplayName, previousHiddenReason })
}

func patchFields(t *testing.T, patch []byte) map[string]any {
	t.Helper()
	fields := map[string]any{}
	if string(patch) == "null" {
		return fields
	}
	if err := json.Unmarshal(patch, &fields); err != nil {
		t.Fatalf("patch %s: %v", patch, err)
	}
	return fields
}

func TestDisplayNameAndHiddenReasonOnWholeEntity(t *testing.T) {
	tests := []struct {
		name                              string
		displayName, hiddenReason         string
		wantDisplayName, wantHiddenReason string
	}{
		{name: "neither flag keeps both", wantDisplayName: "Before", wantHiddenReason: "Archived"},
		{name: "display name", displayName: "After", wantDisplayName: "After", wantHiddenReason: "Archived"},
		{name: "hidden reason", hiddenReason: "Retired", wantDisplayName: "Before", wantHiddenReason: "Retired"},
		{name: "hidden reason cleared", hiddenReason: "-", wantDisplayName: "Before"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			setDisplayNameAndHiddenReasonFlags(t, test.displayName, test.hiddenReason)
			space := goclientnew.Space{DisplayName: "Before", HiddenReason: "Archived"}
			setDisplayNameAndHiddenReason(&space.DisplayName, &space.HiddenReason)
			if space.DisplayName != test.wantDisplayName || space.HiddenReason != test.wantHiddenReason {
				t.Errorf("got DisplayName %q HiddenReason %q, want %q and %q",
					space.DisplayName, space.HiddenReason, test.wantDisplayName, test.wantHiddenReason)
			}
		})
	}
}

func TestDisplayNameAndHiddenReasonInPatch(t *testing.T) {
	t.Run("neither flag leaves the patch alone", func(t *testing.T) {
		setDisplayNameAndHiddenReasonFlags(t, "", "")
		if withDisplayNameAndHiddenReason(nil) != nil {
			t.Error("a nil enhancer became one that adds fields")
		}
		patch, err := BuildPatchDataWithPermissions(nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		if string(patch) != "null" {
			t.Errorf("patch is %s, want null", patch)
		}
	})
	t.Run("flags are added beside what the enhancer adds", func(t *testing.T) {
		setDisplayNameAndHiddenReasonFlags(t, "Checkout", "Archived")
		patch, err := BuildPatchDataWithPermissions(func(patchMap map[string]interface{}) {
			patchMap["Description"] = "from the enhancer"
		}, nil)
		if err != nil {
			t.Fatal(err)
		}
		fields := patchFields(t, patch)
		for field, want := range map[string]string{"DisplayName": "Checkout", "HiddenReason": "Archived", "Description": "from the enhancer"} {
			if fields[field] != want {
				t.Errorf("%s is %v, want %q", field, fields[field], want)
			}
		}
	})
	t.Run("hidden reason cleared", func(t *testing.T) {
		setDisplayNameAndHiddenReasonFlags(t, "", "-")
		patch, err := BuildPatchDataWithPermissions(nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		fields := patchFields(t, patch)
		if reason, ok := fields["HiddenReason"]; !ok || reason != "" {
			t.Errorf("HiddenReason is %v (present %v), want an empty string", reason, ok)
		}
		if _, ok := fields["DisplayName"]; ok {
			t.Error("DisplayName is in the patch without --display-name")
		}
	})
}

func TestSpaceAttributeFlags(t *testing.T) {
	filterID := uuid.New()
	tests := []struct {
		name            string
		values          spaceAttributeFlagValues
		wantWhere       string
		wantFilter      *uuid.UUID
		wantPatchFields map[string]any
	}{
		{
			name:       "neither flag keeps both",
			wantWhere:  "Slug = 'before'",
			wantFilter: &filterID, wantPatchFields: map[string]any{},
		},
		{
			name:       "where expression",
			values:     spaceAttributeFlagValues{whereAttribute: "Labels.Tier = 'platform'"},
			wantWhere:  "Labels.Tier = 'platform'",
			wantFilter: &filterID, wantPatchFields: map[string]any{"WhereAttribute": "Labels.Tier = 'platform'"},
		},
		{
			name:            "both cleared",
			values:          spaceAttributeFlagValues{whereAttribute: "-", attributeFilter: "-"},
			wantPatchFields: map[string]any{"WhereAttribute": "", "AttributeFilterID": nil},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			previousFilter := filterID
			space := goclientnew.Space{WhereAttribute: "Slug = 'before'", AttributeFilterID: &previousFilter}
			if err := test.values.apply(&space); err != nil {
				t.Fatal(err)
			}
			if space.WhereAttribute != test.wantWhere {
				t.Errorf("WhereAttribute is %q, want %q", space.WhereAttribute, test.wantWhere)
			}
			if (space.AttributeFilterID == nil) != (test.wantFilter == nil) ||
				(space.AttributeFilterID != nil && *space.AttributeFilterID != *test.wantFilter) {
				t.Errorf("AttributeFilterID is %v, want %v", space.AttributeFilterID, test.wantFilter)
			}

			enhancer, err := test.values.patchEnhancer()
			if err != nil {
				t.Fatal(err)
			}
			patch := map[string]interface{}{}
			enhancer(patch)
			if len(patch) != len(test.wantPatchFields) {
				t.Errorf("patch is %v, want %v", patch, test.wantPatchFields)
			}
			for field, want := range test.wantPatchFields {
				if got, ok := patch[field]; !ok || got != want {
					t.Errorf("patch %s is %v (present %v), want %v", field, got, ok, want)
				}
			}
		})
	}
}

// entityFieldFlags are flags that set one field of the entity a command writes, by the commands
// that must carry them.
var entityFieldFlags = map[string][]string{
	"space create":       {"where-attribute", "attribute-filter", "where-trigger", "trigger-filter"},
	"space update":       {"where-attribute", "attribute-filter", "where-trigger", "trigger-filter"},
	"variant create":     {"release-target", "permission"},
	"unit update":        {"toolchain", "provider"},
	"link create":        {"update-type"},
	"view update":        {"of"},
	"invocation create":  {"parameter"},
	"invocation update":  {"parameter"},
	"attestation create": {"release"},
}

func TestEntityFieldFlagsAreRegistered(t *testing.T) {
	for path, flags := range entityFieldFlags {
		cmd := findCommand(t, path)
		for _, flag := range flags {
			if cmd.Flags().Lookup(flag) == nil {
				t.Errorf("cub %s has no --%s", path, flag)
			}
		}
	}
}

// A flag for a field that cannot change after creation would only ever restate the value.
func TestUpdateHasNoFlagForImmutableField(t *testing.T) {
	for path, flag := range map[string]string{"link update": "update-type"} {
		if findCommand(t, path).Flags().Lookup(flag) != nil {
			t.Errorf("cub %s has --%s, which names an immutable field", path, flag)
		}
	}
}

// Every create and update that sets labels writes an entity with a HiddenReason, and all but a
// Release have a DisplayName. The exceptions are the two the identity provider holds, whose
// positional argument is the name.
func TestCreateAndUpdateSetDisplayNameAndHiddenReason(t *testing.T) {
	withoutDisplayName := map[string]bool{"cub release update": true}
	heldByIdentityProvider := map[string]bool{"cub organization create": true, "cub organization-member create": true}
	checked := 0
	var visit func(cmd *cobra.Command)
	visit = func(cmd *cobra.Command) {
		for _, child := range cmd.Commands() {
			visit(child)
		}
		if name := cmd.Name(); name != "create" && name != "update" {
			return
		}
		path := cmd.CommandPath()
		if cmd.Flags().Lookup("label") == nil || heldByIdentityProvider[path] {
			return
		}
		checked++
		if cmd.Flags().Lookup("hidden-reason") == nil {
			t.Errorf("%s has --label and no --hidden-reason", path)
		}
		if hasDisplayName := cmd.Flags().Lookup("display-name") != nil; hasDisplayName == withoutDisplayName[path] {
			t.Errorf("%s: --display-name registered is %v", path, hasDisplayName)
		}
	}
	visit(rootCmd)
	if checked < 30 {
		t.Errorf("checked %d create and update commands, expected the whole tree", checked)
	}
}
