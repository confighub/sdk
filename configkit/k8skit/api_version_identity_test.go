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

// A resource keeps its identity when its apiVersion changes: an upgrade that moves a SecretStore
// from external-secrets.io/v1beta1 to v1 changes what its fields mean, not which SecretStore it
// is. Everything recorded about it under the old version -- protection, guards, the history of
// who wrote each path -- has to keep applying to it under the new one.

func TestRemoveAPIVersionFromResourceType(t *testing.T) {
	provider := k8skit.NewK8sResourceProvider()
	for _, tc := range []struct {
		resourceType api.ResourceType
		want         api.ResourceType
	}{
		{"apps/v1/Deployment", "apps/Deployment"},
		{"v1/Service", "Service"},
		{"external-secrets.io/v1beta1/SecretStore", "external-secrets.io/SecretStore"},
		{"autoscaling/v2/HorizontalPodAutoscaler", "autoscaling/HorizontalPodAutoscaler"},
		{"iam.services.k8s.aws/v1alpha1/Role", "iam.services.k8s.aws/Role"},
		// A version is whatever name a CustomResourceDefinition gives it: found by position,
		// not by form.
		{"example.com/v1b1/Widget", "example.com/Widget"},
		{"example.com/v2a3/Widget", "example.com/Widget"},
		{"example.com/stable/Widget", "example.com/Widget"},
		{"example.com/v1/V1", "example.com/V1"},
		// Not an apiVersion and a kind.
		{"*", "*"},
		{"Widget", "Widget"},
	} {
		assert.Equal(t, tc.want, provider.RemoveAPIVersionFromResourceType(tc.resourceType), tc.resourceType)
	}
}

func deploymentInfo(resourceType api.ResourceType, namespace string) api.ResourceInfo {
	return api.ResourceInfo{
		ResourceType:             resourceType,
		ResourceName:             api.ResourceName(namespace + "/app"),
		ResourceNameWithoutScope: "app",
		ResourceCategory:         "Kubernetes",
	}
}

// Protection recorded while the Deployment was apps/v1beta1 still withholds an upstream change
// now that the document is apps/v1.
func TestPatch_ProtectionRecordedUnderAnotherAPIVersion(t *testing.T) {
	provider := k8skit.NewK8sResourceProvider()
	parsed, err := gaby.ParseAll([]byte(conflictTestBase))
	require.NoError(t, err)
	protection := api.ResourceMutationList{{
		Resource:             deploymentInfo("apps/v1beta1/Deployment", "default"),
		ResourceMutationInfo: api.MutationInfo{MutationType: api.MutationTypeUpdate},
		PathMutationMap: api.MutationMap{
			"spec.replicas": api.MutationInfo{Protected: true},
		},
	}}

	patched, conflicts, err := yamlkit.PatchMutations(parsed, protection, deploymentPatchUpdate("5\n", "nginx:1.20\n"), nil, provider, nil)
	require.NoError(t, err)

	c := findConflict(t, conflicts, "app", "spec.replicas")
	assert.Equal(t, api.ConflictReasonProtectedPath, c.Reason)
	replicas, _, err := yamlkit.YamlSafePathGetValue[int](patched[0], "spec.replicas", false)
	require.NoError(t, err)
	assert.Equal(t, 1, replicas, "the protected value is kept")
	image, _, err := yamlkit.YamlSafePathGetValue[string](patched[0], "spec.template.spec.containers.0.image", false)
	require.NoError(t, err)
	assert.Equal(t, "nginx:1.20", image, "an unprotected path is still patched")
}

// A patch computed from an upstream still at the old version applies to a downstream that has
// moved to the new one.
func TestPatch_PatchRecordedUnderAnotherAPIVersion(t *testing.T) {
	provider := k8skit.NewK8sResourceProvider()
	parsed, err := gaby.ParseAll([]byte(conflictTestBase))
	require.NoError(t, err)
	patch := deploymentPatchUpdate("5\n", "nginx:1.20\n")
	patch[0].Resource.ResourceType = "apps/v1beta1/Deployment"

	patched, conflicts, err := yamlkit.PatchMutations(parsed, nil, patch, nil, provider, nil)
	require.NoError(t, err)
	assert.Empty(t, conflicts)

	replicas, _, err := yamlkit.YamlSafePathGetValue[int](patched[0], "spec.replicas", false)
	require.NoError(t, err)
	assert.Equal(t, 5, replicas)
	apiVersion, _, err := yamlkit.YamlSafePathGetValue[string](patched[0], "apiVersion", false)
	require.NoError(t, err)
	assert.Equal(t, "apps/v1", apiVersion, "the document's own version is untouched")
}

// The changes made after a version change are recorded on the resource's existing entry, which
// then names the resource by its current type.
func TestAddMutations_RecordsTheCurrentAPIVersion(t *testing.T) {
	provider := k8skit.NewK8sResourceProvider()
	recorded := api.ResourceMutationList{{
		Resource:             deploymentInfo("apps/v1beta1/Deployment", "default"),
		ResourceMutationInfo: api.MutationInfo{MutationType: api.MutationTypeAdd, Index: 1},
		PathMutationMap: api.MutationMap{
			"spec.replicas": api.MutationInfo{MutationType: api.MutationTypeUpdate, Index: 1, Value: "3\n", Protected: true},
		},
	}}
	changes := api.ResourceMutationList{{
		Resource:             deploymentInfo("apps/v1/Deployment", "default"),
		ResourceMutationInfo: api.MutationInfo{MutationType: api.MutationTypeUpdate, Index: 2},
		PathMutationMap: api.MutationMap{
			"apiVersion": api.MutationInfo{MutationType: api.MutationTypeUpdate, Index: 2, Value: "apps/v1\n"},
		},
	}}

	merged, hasMutations := yamlkit.AddMutations(provider, recorded, changes)
	assert.True(t, hasMutations)
	require.Len(t, merged, 1, "one resource, one entry")
	assert.Equal(t, api.ResourceType("apps/v1/Deployment"), merged[0].Resource.ResourceType)
	assert.Equal(t, api.MutationTypeAdd, merged[0].ResourceMutationInfo.MutationType)
	assert.True(t, merged[0].PathMutationMap["spec.replicas"].Protected)
	assert.Contains(t, merged[0].PathMutationMap, api.ResolvedPath("apiVersion"))
}

// A record written before resources were identified without their versions holds the resource
// twice, the second entry starting at the version change. Folded, it is one entry that keeps
// the protection from the first and the later writes from the second.
func TestCanonicalizeStoredMutationPaths_FoldsAPIVersions(t *testing.T) {
	provider := k8skit.NewK8sResourceProvider()
	parsed, err := gaby.ParseAll([]byte(conflictTestBase))
	require.NoError(t, err)
	service := api.ResourceMutation{
		Resource: api.ResourceInfo{
			ResourceType: "v1/Service", ResourceName: "default/app", ResourceNameWithoutScope: "app",
		},
		ResourceMutationInfo: api.MutationInfo{MutationType: api.MutationTypeAdd, Index: 1},
	}
	stored := api.ResourceMutationList{
		{
			Resource:             deploymentInfo("apps/v1beta1/Deployment", "default"),
			ResourceMutationInfo: api.MutationInfo{MutationType: api.MutationTypeAdd, Index: 1},
			PathMutationMap: api.MutationMap{
				"spec.replicas":       api.MutationInfo{MutationType: api.MutationTypeUpdate, Index: 3, Value: "3\n", Protected: true},
				"metadata.labels.app": api.MutationInfo{MutationType: api.MutationTypeUpdate, Index: 1, Value: "app\n"},
			},
			AliasesWithoutScopes: map[api.ResourceName]struct{}{"old-app": {}},
		},
		service,
		{
			Resource:             deploymentInfo("apps/v1/Deployment", "default"),
			ResourceMutationInfo: api.MutationInfo{MutationType: api.MutationTypeUpdate, Index: 5},
			PathMutationMap: api.MutationMap{
				"apiVersion":    api.MutationInfo{MutationType: api.MutationTypeUpdate, Index: 5, Value: "apps/v1\n"},
				"spec.replicas": api.MutationInfo{MutationType: api.MutationTypeUpdate, Index: 6, Value: "4\n"},
			},
		},
	}

	require.True(t, yamlkit.StoredMutationPathsNeedRewriting(stored, provider))
	folded, changed := yamlkit.CanonicalizeStoredMutationPaths(stored, parsed, provider)
	require.True(t, changed)
	require.Len(t, folded, 2)

	deployment := folded[0]
	assert.Equal(t, api.ResourceType("apps/v1/Deployment"), deployment.Resource.ResourceType, "the current type")
	assert.Equal(t, api.MutationTypeAdd, deployment.ResourceMutationInfo.MutationType, "the resource was added, then updated")
	replicas := deployment.PathMutationMap["spec.replicas"]
	assert.Equal(t, "4\n", replicas.Value, "the later write wins")
	assert.Equal(t, int64(6), replicas.Index)
	assert.True(t, replicas.Protected, "protection survives from the earlier entry")
	assert.Contains(t, deployment.PathMutationMap, api.ResolvedPath("metadata.labels.app"))
	assert.Contains(t, deployment.PathMutationMap, api.ResolvedPath("apiVersion"))
	assert.Contains(t, deployment.AliasesWithoutScopes, api.ResourceName("old-app"))
	assert.Equal(t, service.Resource, folded[1].Resource, "the order the resources were first recorded in")

	assert.False(t, yamlkit.StoredMutationPathsNeedRewriting(folded, provider), "nothing left to fold")
}

// A later entry that deletes or replaces the whole resource supersedes the earlier one.
func TestCanonicalizeStoredMutationPaths_FoldedDeleteSupersedes(t *testing.T) {
	provider := k8skit.NewK8sResourceProvider()
	stored := api.ResourceMutationList{
		{
			Resource:             deploymentInfo("apps/v1beta1/Deployment", "default"),
			ResourceMutationInfo: api.MutationInfo{MutationType: api.MutationTypeAdd, Index: 1},
			PathMutationMap: api.MutationMap{
				"spec.replicas": api.MutationInfo{MutationType: api.MutationTypeUpdate, Index: 1, Protected: true},
			},
		},
		{
			Resource:             deploymentInfo("apps/v1/Deployment", "default"),
			ResourceMutationInfo: api.MutationInfo{MutationType: api.MutationTypeDelete, Index: 2},
		},
	}
	folded, changed := yamlkit.CanonicalizeStoredMutationPaths(stored, nil, provider)
	require.True(t, changed)
	require.Len(t, folded, 1)
	assert.Equal(t, stored[1], folded[0])
}

// Two Deployments named app in different namespaces are two resources, not one recorded twice,
// even when their types are written in different versions.
func TestCanonicalizeStoredMutationPaths_KeepsResourcesInOtherNamespaces(t *testing.T) {
	provider := k8skit.NewK8sResourceProvider()
	stored := api.ResourceMutationList{
		{
			Resource:             deploymentInfo("apps/v1beta1/Deployment", "blue"),
			ResourceMutationInfo: api.MutationInfo{MutationType: api.MutationTypeAdd, Index: 1},
		},
		{
			Resource:             deploymentInfo("apps/v1/Deployment", "green"),
			ResourceMutationInfo: api.MutationInfo{MutationType: api.MutationTypeAdd, Index: 2},
		},
	}
	assert.False(t, yamlkit.StoredMutationPathsNeedRewriting(stored, provider))
	folded, changed := yamlkit.CanonicalizeStoredMutationPaths(stored, nil, provider)
	assert.False(t, changed)
	assert.Equal(t, stored, folded)
}
