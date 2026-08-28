// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package k8skit_test

import (
	"testing"

	"github.com/confighub/sdk/configkit/k8skit"
	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/third_party/gaby"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A source that removes a whole mapping records one coarse Delete at the mapping, not a Delete
// per key. The target may have added its own keys under that mapping since, and protected them.
// Those keys have to survive: protection is the only thing preserving a local override on the
// default merge path, where subtraction is off.

const deleteProtectionBase = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: app
  namespace: default
  annotations:
    owner: platform
spec:
  replicas: 1
`

// The source dropped the annotations block entirely.
const deleteProtectionSource = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: app
  namespace: default
spec:
  replicas: 1
`

// The target kept owner and added team of its own.
const deleteProtectionTarget = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: app
  namespace: default
  annotations:
    owner: platform
    team: infra
spec:
  replicas: 1
`

func deleteProtectionInputs(t *testing.T) (gaby.Container, api.ResourceMutationList, api.ResourceMutationList) {
	t.Helper()
	provider := k8skit.NewK8sResourceProvider()
	baseP, err := gaby.ParseAll([]byte(deleteProtectionBase))
	require.NoError(t, err)
	srcP, err := gaby.ParseAll([]byte(deleteProtectionSource))
	require.NoError(t, err)
	tgtP, err := gaby.ParseAll([]byte(deleteProtectionTarget))
	require.NoError(t, err)

	patch, err := yamlkit.ComputeMutations(baseP, srcP, 1, provider)
	require.NoError(t, err)

	protection := api.ResourceMutationList{{
		Resource: api.ResourceInfo{
			ResourceType:             "apps/v1/Deployment",
			ResourceName:             "default/app",
			ResourceNameWithoutScope: "app",
			ResourceCategory:         "Kubernetes",
		},
		ResourceMutationInfo: api.MutationInfo{MutationType: api.MutationTypeUpdate},
		PathMutationMap: api.MutationMap{
			"metadata.annotations.team": api.MutationInfo{
				MutationType: api.MutationTypeAdd,
				Protected:    true,
				Value:        "infra\n",
			},
		},
	}}
	return tgtP, protection, patch
}

// The premise: removing the block is recorded coarsely, at the mapping itself.
func TestComputeMutations_RemovingAMappingIsOneCoarseDelete(t *testing.T) {
	_, _, patch := deleteProtectionInputs(t)
	require.Len(t, patch, 1)
	info, ok := patch[0].PathMutationMap["metadata.annotations"]
	require.True(t, ok, "expected a coarse Delete at metadata.annotations, got %v", patch[0].PathMutationMap)
	assert.Equal(t, api.MutationTypeDelete, info.MutationType)
}

// A protected key under a coarse Delete survives it, and the deletion the source did ask for
// still happens to the keys nobody claimed.
func TestPatch_CoarseDeleteSparesProtectedChild(t *testing.T) {
	provider := k8skit.NewK8sResourceProvider()
	tgtP, protection, patch := deleteProtectionInputs(t)

	patched, conflicts, err := yamlkit.PatchMutations(tgtP, protection, patch, nil, provider, nil)
	require.NoError(t, err)

	got := string(patched.Bytes())
	assert.Contains(t, got, "team: infra", "protected annotation must survive the coarse Delete")
	assert.NotContains(t, got, "owner: platform", "unprotected annotation was the source's to remove")

	c := findConflict(t, conflicts, "app", "metadata.annotations.team")
	assert.Equal(t, api.ConflictReasonProtectedPath, c.Reason)
}

// Nothing protected under it: the coarse Delete still removes the whole mapping.
func TestPatch_CoarseDeleteUnprotectedRemovesWholeMapping(t *testing.T) {
	provider := k8skit.NewK8sResourceProvider()
	tgtP, _, patch := deleteProtectionInputs(t)

	patched, conflicts, err := yamlkit.PatchMutations(tgtP, nil, patch, nil, provider, nil)
	require.NoError(t, err)

	got := string(patched.Bytes())
	assert.NotContains(t, got, "annotations:")
	assert.NotContains(t, got, "team: infra")
	assert.Empty(t, conflicts)
}

// guardTableForApp is guardTable for the deployment these tests use.
func guardTableForApp(guards map[api.ResolvedPath]map[string]string) api.PathAnnotationList {
	entry := api.ResourcePathAnnotations{
		Resource: api.ResourceInfo{
			ResourceType:             "apps/v1/Deployment",
			ResourceName:             "default/app",
			ResourceNameWithoutScope: "app",
		},
		PathAnnotationMap: map[api.ResolvedPath]api.PathAnnotations{},
	}
	for path, entries := range guards {
		entry.PathAnnotationMap[path] = api.PathAnnotations{api.AnnotationKindGuard: entries}
	}
	return api.PathAnnotationList{entry}
}

// A guard under a coarse Delete has to stop it for the same reason protection does. The two
// mechanisms answer different questions -- protection says a path was claimed, a guard says why
// and who may clear it -- but neither can be consulted by walking up from the Delete's own path,
// which is the only walk the patch loop does.
func TestPatch_CoarseDeleteSparesGuardedChild(t *testing.T) {
	provider := k8skit.NewK8sResourceProvider()
	tgtP, _, patch := deleteProtectionInputs(t)

	guards := &yamlkit.GuardFilter{
		Annotations: guardTableForApp(map[api.ResolvedPath]map[string]string{
			"metadata.annotations.team": {"owner": "platform-team"},
		}),
	}

	patched, conflicts, err := yamlkit.PatchMutationsGuarded(tgtP, nil, patch, nil, guards, provider, nil)
	require.NoError(t, err)

	got := string(patched.Bytes())
	assert.Contains(t, got, "team: infra", "a guarded annotation must survive the coarse Delete")
	assert.NotContains(t, got, "owner: platform", "the unguarded annotation was the source's to remove")

	require.NotEmpty(t, conflicts, "the withheld delete has to be reported")
	assert.Equal(t, api.ConflictReasonGuarded, conflicts[0].Reason)
}

// Cleared for the reason, the same Delete goes through whole.
func TestPatch_CoarseDeleteClearedGuardRemovesMapping(t *testing.T) {
	provider := k8skit.NewK8sResourceProvider()
	tgtP, _, patch := deleteProtectionInputs(t)

	guards := &yamlkit.GuardFilter{
		Annotations: guardTableForApp(map[api.ResolvedPath]map[string]string{
			"metadata.annotations.team": {"owner": "platform-team"},
		}),
		Clearance: api.Clearance{{
			Key: "owner", Operator: api.ClearanceOperatorIn, Values: []string{"platform-team"},
		}},
	}

	patched, conflicts, err := yamlkit.PatchMutationsGuarded(tgtP, nil, patch, nil, guards, provider, nil)
	require.NoError(t, err)

	got := string(patched.Bytes())
	assert.NotContains(t, got, "team: infra")
	assert.NotContains(t, got, "annotations:")
	assert.Empty(t, conflicts)
}
