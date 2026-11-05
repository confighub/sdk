// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package api_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/confighub/sdk/core/function/api"
)

// Every guard a clearance does not cover is reported, the ones it forbids first and each group in
// key order, so the same write withheld twice reports the same guards.
func TestClearanceWithheldReportsEveryGuardInOrder(t *testing.T) {
	guards := map[string]string{"zone": "a", "owner": "platform", "policy-exception": "host-network", "team": "web"}
	clearance := api.Clearance{
		{Key: "team", Operator: api.ClearanceOperatorExists},
		{Key: "zone", Operator: api.ClearanceOperatorDoesNotExist},
	}
	want := []api.WithheldGuard{
		{Key: "zone", Value: "a", Precondition: true},
		{Key: "owner", Value: "platform"},
		{Key: "policy-exception", Value: "host-network"},
	}
	for range 20 {
		assert.Equal(t, want, clearance.Withheld(guards))
		admitted, first := clearance.Admits(guards)
		assert.False(t, admitted)
		assert.Equal(t, want[0], first)
	}
	assert.Empty(t, clearance.Withheld(map[string]string{"team": "web"}))
}

func TestConflictDismissalEntries(t *testing.T) {
	entries, err := api.ConflictDismissalEntries(api.ConflictReasonSubtracted, nil)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"Subtracted": ""}, entries)

	guards, err := api.ParseGuardSelectors([]string{"owner", "policy-exception=host-network"})
	require.NoError(t, err)
	entries, err = api.ConflictDismissalEntries(api.ConflictReasonGuarded, guards)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"Guarded-owner": "", "Guarded-policy-exception": "host-network"}, entries)

	reason, guardKey := api.ParseConflictDismissalKey("Guarded-policy-exception")
	assert.Equal(t, api.ConflictReasonGuarded, reason)
	assert.Equal(t, "policy-exception", guardKey)

	_, err = api.ConflictDismissalEntries(api.ConflictReasonSubtracted, guards)
	assert.Error(t, err, "a guard is not the cause of a Subtracted conflict")
	_, err = api.ConflictDismissalEntries("Whatever", nil)
	assert.Error(t, err)
	_, err = api.ParseGuardSelectors([]string{"bad key"})
	assert.Error(t, err)
}

func TestRemoveConflictDismissals(t *testing.T) {
	resource := api.ResourceInfo{ResourceType: "apps/v1/Deployment", ResourceName: "prod/web", ResourceNameWithoutScope: "web"}
	var table api.PathAnnotationList
	table = api.SetConflictDismissal(nil, table, resource, "",
		map[string]string{"Subtracted": ""})
	table = api.SetConflictDismissal(nil, table, resource, "spec.replicas",
		map[string]string{"Subtracted": "", "Guarded-owner": "", "Guarded-team": "web"})
	table = api.SetConflictDismissal(nil, table, resource, "spec.replicas",
		map[string]string{api.ConflictDismissalKey(api.ConflictReasonProtectedPath, ""): ""})
	require.True(t, api.HasConflictDismissals(table))
	assert.False(t, api.HasGuards(table))

	atReplicas := func(path api.ResolvedPath) bool { return path == "spec.replicas" }
	guards, err := api.ParseGuardSelectors([]string{"owner"})
	require.NoError(t, err)
	// A guard named with a value removes only the dismissal for that value.
	otherTeam, err := api.ParseGuardSelectors([]string{"team=other"})
	require.NoError(t, err)
	assert.Equal(t, 0, api.RemoveConflictDismissals(nil, table, resource, atReplicas, api.ConflictReasonGuarded, otherTeam))
	assert.Equal(t, 1, api.RemoveConflictDismissals(nil, table, resource, atReplicas, api.ConflictReasonGuarded, guards))
	assert.Equal(t, map[string]string{"Subtracted": "", "Guarded-team": "web", "ProtectedPath": ""},
		table[0].PathAnnotationMap["spec.replicas"][api.AnnotationKindConflictDismissal])

	// A reason alone removes it with every guard of it.
	assert.Equal(t, 1, api.RemoveConflictDismissals(nil, table, resource, atReplicas, api.ConflictReasonGuarded, nil))
	// No reason removes everything at the paths selected, the resource's own included.
	everywhere := func(api.ResolvedPath) bool { return true }
	assert.Equal(t, 3, api.RemoveConflictDismissals(nil, table, resource, everywhere, "", nil))
	assert.False(t, api.HasConflictDismissals(table))
}

// A clone leaves the upstream's dismissals behind and keeps its guards.
func TestWithoutAnnotationKind(t *testing.T) {
	resource := api.ResourceInfo{ResourceType: "apps/v1/Deployment", ResourceName: "prod/web"}
	table := api.PathAnnotationList{{
		Resource: resource,
		PathAnnotationMap: map[api.ResolvedPath]api.PathAnnotations{
			"spec.replicas": {api.AnnotationKindConflictDismissal: {"Subtracted": ""}},
			"spec.template": {api.AnnotationKindGuard: {"owner": "platform"}, api.AnnotationKindConflictDismissal: {"Subtracted": ""}},
		},
	}, {
		Resource:            api.ResourceInfo{ResourceType: "v1/Service", ResourceName: "prod/web"},
		ResourceAnnotations: api.PathAnnotations{api.AnnotationKindConflictDismissal: {"Subtracted": ""}},
	}}
	stripped := api.WithoutAnnotationKind(table, api.AnnotationKindConflictDismissal)
	assert.Equal(t, api.PathAnnotationList{{
		Resource: resource,
		PathAnnotationMap: map[api.ResolvedPath]api.PathAnnotations{
			"spec.template": {api.AnnotationKindGuard: {"owner": "platform"}},
		},
	}}, stripped)
	assert.Len(t, table[0].PathAnnotationMap, 2, "the original is left as it was")
	assert.Nil(t, api.WithoutAnnotationKind(table[1:], api.AnnotationKindConflictDismissal))
}
