// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package generic

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/third_party/gaby"
)

// vet-no-merge-conflicts is the policy hook for unresolved merges: whether a Unit whose
// last merge could not apply part of its patch should be allowed to reach a cluster is a
// question for the Space, not for the merge, so it is answered by a Trigger.
func TestVetNoMergeConflicts(t *testing.T) {
	parsed, err := gaby.ParseAll([]byte("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: cm\n"))
	require.NoError(t, err)

	conflicts := api.MutationConflictList{
		{
			Reason:   api.ConflictReasonUnresolvedPath,
			Resource: api.ResourceInfo{ResourceName: "ns/web", ResourceType: "apps/v1/Deployment"},
			Path:     "spec.replicas",
			Source:   api.MutationInfo{MutationType: api.MutationTypeUpdate},
		},
		{
			Reason:   api.ConflictReasonSubtracted,
			Resource: api.ResourceInfo{ResourceName: "ns/web", ResourceType: "apps/v1/Deployment"},
			Path:     "spec.template.spec.containers.0.image",
			Source:   api.MutationInfo{MutationType: api.MutationTypeUpdate},
		},
	}

	t.Run("a unit with no conflicts passes", func(t *testing.T) {
		_, result, err := genericFnVetNoMergeConflicts(nil, &api.FunctionContext{}, parsed, nil)
		require.NoError(t, err)
		assert.True(t, result.(api.ValidationResult).Passed)
	})

	t.Run("any outstanding conflict fails when no reasons are given", func(t *testing.T) {
		_, result, err := genericFnVetNoMergeConflicts(nil,
			&api.FunctionContext{Conflicts: conflicts}, parsed, nil)
		require.NoError(t, err)
		validation := result.(api.ValidationResult)
		assert.False(t, validation.Passed)
		assert.Len(t, validation.Issues, 2, "each conflict is reported, so the gate says which")
		assert.Contains(t, validation.Issues[0].Message, "ns/web spec.replicas")
	})

	t.Run("only the named reasons fail", func(t *testing.T) {
		// Subtracted means the downstream's own value was preserved on purpose, which a
		// Space may well not want to block on. Naming the reasons is how it says so.
		_, result, err := genericFnVetNoMergeConflicts(nil,
			&api.FunctionContext{Conflicts: conflicts}, parsed,
			[]api.FunctionArgument{{Value: "UnresolvedPath"}})
		require.NoError(t, err)
		validation := result.(api.ValidationResult)
		assert.False(t, validation.Passed)
		require.Len(t, validation.Issues, 1)
		assert.Equal(t, "UnresolvedPath", validation.Issues[0].Identifier)
	})

	t.Run("a reason nothing matches passes", func(t *testing.T) {
		_, result, err := genericFnVetNoMergeConflicts(nil,
			&api.FunctionContext{Conflicts: conflicts}, parsed,
			[]api.FunctionArgument{{Value: "DeleteShadowed"}})
		require.NoError(t, err)
		assert.True(t, result.(api.ValidationResult).Passed)
	})

	t.Run("reasons are trimmed and may be listed", func(t *testing.T) {
		_, result, err := genericFnVetNoMergeConflicts(nil,
			&api.FunctionContext{Conflicts: conflicts}, parsed,
			[]api.FunctionArgument{{Value: " UnresolvedPath , Subtracted "}})
		require.NoError(t, err)
		assert.Len(t, result.(api.ValidationResult).Issues, 2)
	})
}
