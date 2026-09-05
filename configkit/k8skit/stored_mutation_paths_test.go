// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package k8skit

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/third_party/gaby"
)

// A record written before a resource type was registered addresses that type's array elements by
// position. Once the type is registered a freshly computed diff names them by merge key, and the
// two forms do not find each other -- so the stored record is read with today's merge keys, at
// the point it meets the document its paths address.

const runnerSetYAML = `apiVersion: actions.github.com/v1alpha1
kind: AutoscalingRunnerSet
metadata:
  name: gha-runner-scale-set
  namespace: ns
spec:
  template:
    spec:
      containers:
      - name: runner
        image: ghcr.io/actions/runner:2.320.0
      - name: dind
        image: docker:dind
`

func runnerSetRecord(paths api.MutationMap) api.ResourceMutationList {
	return api.ResourceMutationList{{
		Resource: api.ResourceInfo{
			ResourceType:             "actions.github.com/v1alpha1/AutoscalingRunnerSet",
			ResourceName:             "ns/gha-runner-scale-set",
			ResourceNameWithoutScope: "gha-runner-scale-set",
		},
		PathMutationMap: paths,
	}}
}

func parsedRunnerSet(t *testing.T) gaby.Container {
	t.Helper()
	parsed, err := gaby.ParseAll([]byte(runnerSetYAML))
	require.NoError(t, err)
	return parsed
}

func TestStoredPositionalPathIsNamedByTodaysMergeKeys(t *testing.T) {
	provider := NewK8sResourceProvider()
	stored := runnerSetRecord(api.MutationMap{
		"spec.template.spec.containers.1.image": {Index: 3, MutationType: api.MutationTypeUpdate, Protected: true, Value: "docker:dind\n"},
	})

	require.True(t, yamlkit.StoredMutationPathsNeedRewriting(stored, provider))
	rewritten, changed := yamlkit.CanonicalizeStoredMutationPaths(stored, parsedRunnerSet(t), provider)
	require.True(t, changed)

	info, found := rewritten[0].PathMutationMap["spec.template.spec.containers.?name=dind.image"]
	require.True(t, found, "position 1 is read off the document as the container named dind")
	assert.True(t, info.Protected, "the protection recorded on the stale key is what this is for")
	assert.Len(t, rewritten[0].PathMutationMap, 1)

	// The caller's record is not modified in place: it may be a Revision's, which nothing here
	// is entitled to rewrite.
	_, stillThere := stored[0].PathMutationMap["spec.template.spec.containers.1.image"]
	assert.True(t, stillThere)
}

// The stale key and the key a diff computed after registration produces are the same element.
// Folding them keeps Protected from either, because losing it reopens a path its owner closed.
func TestTheStaleAndTheFreshEntryFoldWithProtectionKept(t *testing.T) {
	provider := NewK8sResourceProvider()
	stored := runnerSetRecord(api.MutationMap{
		"spec.template.spec.containers.0.image":            {Index: 3, MutationType: api.MutationTypeUpdate, Protected: true, Value: "old\n"},
		"spec.template.spec.containers.?name=runner.image": {Index: 7, MutationType: api.MutationTypeUpdate, Value: "ghcr.io/actions/runner:2.320.0\n"},
	})

	rewritten, changed := yamlkit.CanonicalizeStoredMutationPaths(stored, parsedRunnerSet(t), provider)
	require.True(t, changed)
	require.Len(t, rewritten[0].PathMutationMap, 1)

	info := rewritten[0].PathMutationMap["spec.template.spec.containers.?name=runner.image"]
	assert.Equal(t, int64(7), info.Index, "the later entry by MutationNum supplies the value")
	assert.True(t, info.Protected, "and the protection survives from the entry that carried it")
}

// A position in an array with no merge key is the only form there is for that element, so a
// record holding one is not stale and must not cost a parse of the document.
func TestAPositionWithNoMergeKeyIsNotStale(t *testing.T) {
	provider := NewK8sResourceProvider()
	stored := runnerSetRecord(api.MutationMap{
		"spec.template.spec.containers.?name=runner.command.0": {Index: 3, MutationType: api.MutationTypeUpdate, Value: "cp\n"},
	})

	assert.False(t, yamlkit.StoredMutationPathsNeedRewriting(stored, provider))
	_, changed := yamlkit.CanonicalizeStoredMutationPaths(stored, parsedRunnerSet(t), provider)
	assert.False(t, changed)
}

// A type nothing declares merge keys for keeps its positional paths, and asks no document for
// them.
func TestAnUnregisteredTypeKeepsItsPositionalPaths(t *testing.T) {
	provider := NewK8sResourceProvider()
	stored := api.ResourceMutationList{{
		Resource: api.ResourceInfo{ResourceType: "example.com/v1/NobodyDeclaredThis", ResourceName: "ns/thing"},
		PathMutationMap: api.MutationMap{
			"spec.things.0.name": {Index: 3, MutationType: api.MutationTypeUpdate, Value: "x\n"},
		},
	}}

	assert.False(t, yamlkit.StoredMutationPathsNeedRewriting(stored, provider))
}

// The ;@index fallback is dropped without consulting the document, which is the half of this
// that is a string operation.
func TestTheAnchoredFallbackIsDroppedWithoutADocument(t *testing.T) {
	provider := NewK8sResourceProvider()
	stored := runnerSetRecord(api.MutationMap{
		"spec.template.spec.containers.?name=runner;@0.image": {Index: 3, MutationType: api.MutationTypeUpdate, Protected: true, Value: "x\n"},
	})

	require.True(t, yamlkit.StoredMutationPathsNeedRewriting(stored, provider))
	rewritten, changed := yamlkit.CanonicalizeStoredMutationPaths(stored, nil, provider)
	require.True(t, changed)
	_, found := rewritten[0].PathMutationMap["spec.template.spec.containers.?name=runner.image"]
	assert.True(t, found)
}
