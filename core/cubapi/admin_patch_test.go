// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package cubapi

import (
	"encoding/json"
	"testing"

	api "github.com/confighub/sdk/core/function/api"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
)

func patchMap(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("patch is not an object: %v (%s)", err, b)
	}
	return m
}

// A patch carries the fields the caller described and nothing else. A field left
// at its zero value is one the description does not claim, so sending it would
// make the reconcile assert things the caller never said.
func TestMergePatchCarriesOnlyWhatWasDescribed(t *testing.T) {
	b, err := mergePatch[goclientnew.PatchTriggerApplicationMergePatchPlusJSONBody](goclientnew.Trigger{
		Slug:         "autoscaler-not-pinned",
		Description:  "an autoscaler must pin its bounds",
		FunctionName: "vet-cel",
		Warn:         true,
	})
	if err != nil {
		t.Fatalf("mergePatch: %v", err)
	}
	m := patchMap(t, b)
	for k, want := range map[string]any{
		"Slug":         "autoscaler-not-pinned",
		"Description":  "an autoscaler must pin its bounds",
		"FunctionName": "vet-cel",
		"Warn":         true,
	} {
		if m[k] != want {
			t.Errorf("patch[%q] = %v, want %v", k, m[k], want)
		}
	}
	for _, absent := range []string{"Labels", "Disabled", "WhereUnit", "Params", "Protect"} {
		if _, ok := m[absent]; ok {
			t.Errorf("patch carries optional %q, which the description did not set: %v", absent, m[absent])
		}
	}
}

// A required field is always asserted, set or not. The generator leaves
// omitempty off exactly the required fields, so they survive to the patch at
// their zero value -- which is right: the same description has to serve the
// create, and an entity missing a required field would not have been creatable.
// A description that cares about one sets it.
func TestMergePatchAlwaysAssertsRequiredFields(t *testing.T) {
	b, err := mergePatch[goclientnew.PatchTriggerApplicationMergePatchPlusJSONBody](goclientnew.Trigger{
		Slug: "rule",
	})
	if err != nil {
		t.Fatalf("mergePatch: %v", err)
	}
	m := patchMap(t, b)
	for _, required := range []string{"Slug", "Event", "ToolchainType", "FailOpenAfter"} {
		if _, ok := m[required]; !ok {
			t.Errorf("patch omits required %q, so a create built from the same description would fail", required)
		}
	}
}

// A patch built from the entity struct would carry the server's own fields: the
// entity id, its SpaceID, the timestamps. The merge-patch body has no place to
// put them, which is what keeps a zero UUID from being sent for a field the
// caller never touched.
func TestMergePatchDropsServerOwnedFields(t *testing.T) {
	spaceID := goclientnew.UUID(uuid.MustParse("11111111-1111-1111-1111-111111111111"))
	b, err := mergePatch[goclientnew.PatchTriggerApplicationMergePatchPlusJSONBody](goclientnew.Trigger{
		TriggerID: goclientnew.UUID(uuid.MustParse("22222222-2222-2222-2222-222222222222")),
		SpaceID:   spaceID,
		Slug:      "rule",
	})
	if err != nil {
		t.Fatalf("mergePatch: %v", err)
	}
	m := patchMap(t, b)
	for _, absent := range []string{"TriggerID", "SpaceID", "OrganizationID", "CreatedAt", "UpdatedAt"} {
		if _, ok := m[absent]; ok {
			t.Errorf("patch carries server-owned %q = %v", absent, m[absent])
		}
	}
	if m["Slug"] != "rule" {
		t.Errorf("patch dropped the field that was described: %v", m)
	}
}

// A merge patch reads null as "delete this key", so a null anywhere in the body
// erases a field the description never mentioned. The body type is all pointers,
// which makes every unset field a null before stripping.
func TestStripNullsIsRecursive(t *testing.T) {
	in := map[string]any{
		"Slug":  "rule",
		"Gone":  nil,
		"Inner": map[string]any{"Kept": "yes", "Gone": nil},
		"List": []any{
			map[string]any{"Kept": 1, "Gone": nil},
			nil,
		},
		"Deep": map[string]any{"Mid": map[string]any{"Kept": true, "Gone": nil}},
	}
	got := stripNulls(in).(map[string]any)

	if _, ok := got["Gone"]; ok {
		t.Error("top-level null survived")
	}
	if inner := got["Inner"].(map[string]any); len(inner) != 1 || inner["Kept"] != "yes" {
		t.Errorf("nested null survived: %v", inner)
	}
	list := got["List"].([]any)
	if len(list) != 1 {
		t.Errorf("null element survived: %v", list)
	}
	if elem := list[0].(map[string]any); len(elem) != 1 {
		t.Errorf("null inside a slice element survived: %v", elem)
	}
	mid := got["Deep"].(map[string]any)["Mid"].(map[string]any)
	if len(mid) != 1 || mid["Kept"] != true {
		t.Errorf("null two levels down survived: %v", mid)
	}
}

// The nested case, end to end: an Invocation's function arguments are a list of
// objects, so a null inside one is two levels below the patch body.
func TestMergePatchOfNestedInvocationArguments(t *testing.T) {
	b, err := mergePatch[goclientnew.PatchInvocationApplicationMergePatchPlusJSONBody](goclientnew.Invocation{
		Slug: "hpa-range",
		FunctionInvocations: FunctionInvocations(api.FunctionInvocation{
			FunctionName: "set-yq",
			Arguments:    []api.FunctionArgument{{ParameterName: "yq-expression", Value: ".spec.minReplicas = 1"}},
		}),
	})
	if err != nil {
		t.Fatalf("mergePatch: %v", err)
	}
	if bytesContainsNull(b) {
		t.Errorf("patch carries a null, which a merge patch reads as a deletion: %s", b)
	}
	m := patchMap(t, b)
	if m["Slug"] != "hpa-range" {
		t.Errorf("patch = %s", b)
	}
	if _, ok := m["FunctionInvocations"]; !ok {
		t.Errorf("patch dropped the invocations it was given: %s", b)
	}
}

func bytesContainsNull(b []byte) bool {
	var walk func(any) bool
	walk = func(v any) bool {
		switch t := v.(type) {
		case nil:
			return true
		case map[string]any:
			for _, val := range t {
				if walk(val) {
					return true
				}
			}
		case []any:
			for _, val := range t {
				if walk(val) {
					return true
				}
			}
		}
		return false
	}
	var m any
	if err := json.Unmarshal(b, &m); err != nil {
		return false
	}
	return walk(m)
}
