// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package yamlkit

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/third_party/gaby"
)

const setProtectionYAML = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  namespace: default
spec:
  replicas: 3
  template:
    spec:
      containers:
      - name: app
        image: nginx:1.20
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: cm
  namespace: default
data:
  key: val
`

func setProtectionData(t *testing.T) gaby.Container {
	docs, err := gaby.ParseAll([]byte(setProtectionYAML))
	require.NoError(t, err)
	return docs
}

func deploymentResource() api.ResourceInfo {
	return api.ResourceInfo{
		ResourceType:             "apps/v1/Deployment",
		ResourceName:             "default/myapp",
		ResourceNameWithoutScope: "myapp",
	}
}

func deploymentMutations() api.ResourceMutationList {
	return api.ResourceMutationList{
		{
			Resource:             deploymentResource(),
			ResourceMutationInfo: api.MutationInfo{MutationType: api.MutationTypeUpdate, Index: 5},
			PathMutationMap: api.MutationMap{
				"spec.replicas":                 {MutationType: api.MutationTypeUpdate, Index: 5, Value: "3\n"},
				"spec.template.spec.containers": {MutationType: api.MutationTypeUpdate, Index: 7, Value: "<block>"},
			},
		},
	}
}

func TestSetProtectionExactMatch(t *testing.T) {
	mutations := deploymentMutations()
	updated, unresolved := SetProtection(setProtectionData(t), mutations, deploymentResource(),
		map[api.ResolvedPath]bool{"spec.replicas": true}, testProvider)
	assert.Empty(t, unresolved)
	got := updated[0].PathMutationMap["spec.replicas"]
	assert.True(t, got.Protected, "an exact match should have its Protected flag set")
	assert.Equal(t, int64(5), got.Index, "exact match should preserve Index/provenance")
	assert.Equal(t, "3\n", got.Value, "exact match should preserve the existing Value")
}

func TestSetProtectionParentSplitExtractsValue(t *testing.T) {
	mutations := deploymentMutations()
	childPath := api.ResolvedPath("spec.template.spec.containers.0.image")
	updated, unresolved := SetProtection(setProtectionData(t), mutations, deploymentResource(),
		map[api.ResolvedPath]bool{childPath: true}, testProvider)
	assert.Empty(t, unresolved)
	got, ok := updated[0].PathMutationMap[childPath]
	require.True(t, ok, "a new child entry should be spliced in")
	assert.True(t, got.Protected)
	assert.Equal(t, int64(7), got.Index, "child should inherit the ancestor's Index/provenance")
	// Value is extracted from the data at the child path, NOT copied from the ancestor block.
	assert.Equal(t, "nginx:1.20", strings.TrimSpace(got.Value))
	assert.Empty(t, got.Patch, "Patch should be left empty for a freshly-extracted value")
	// Ancestor entry is unchanged.
	assert.False(t, updated[0].PathMutationMap["spec.template.spec.containers"].Protected)
}

func TestSetProtectionResourceLevelFallback(t *testing.T) {
	mutations := api.ResourceMutationList{
		{
			Resource:             api.ResourceInfo{ResourceType: "v1/ConfigMap", ResourceName: "default/cm", ResourceNameWithoutScope: "cm"},
			ResourceMutationInfo: api.MutationInfo{MutationType: api.MutationTypeAdd, Index: 9},
			PathMutationMap:      api.MutationMap{},
		},
	}
	updated, unresolved := SetProtection(setProtectionData(t), mutations, mutations[0].Resource,
		map[api.ResolvedPath]bool{"data.key": true}, testProvider)
	assert.Empty(t, unresolved)
	got := updated[0].PathMutationMap["data.key"]
	assert.True(t, got.Protected)
	assert.Equal(t, int64(9), got.Index, "should inherit the resource-level Index")
	assert.Equal(t, "val", strings.TrimSpace(got.Value))
}

func TestSetProtectionPathNotInData(t *testing.T) {
	mutations := deploymentMutations()
	_, unresolved := SetProtection(setProtectionData(t), mutations, deploymentResource(),
		map[api.ResolvedPath]bool{"spec.doesNotExist": true}, testProvider)
	assert.Equal(t, []api.ResolvedPath{"spec.doesNotExist"}, unresolved)
}

func TestSetProtectionUnmatchedResource(t *testing.T) {
	mutations := deploymentMutations()
	other := api.ResourceInfo{ResourceType: "v1/Service", ResourceName: "default/svc", ResourceNameWithoutScope: "svc"}
	_, unresolved := SetProtection(setProtectionData(t), mutations, other,
		map[api.ResolvedPath]bool{"spec.ports": true}, testProvider)
	assert.Equal(t, []api.ResolvedPath{"spec.ports"}, unresolved)
}

func TestSetProtectionWholeResource(t *testing.T) {
	mutations := deploymentMutations()
	updated, unresolved := SetProtection(setProtectionData(t), mutations, deploymentResource(),
		map[api.ResolvedPath]bool{"": true}, testProvider)
	assert.Empty(t, unresolved)
	assert.True(t, updated[0].ResourceMutationInfo.Protected, "the empty path should protect the resource as a whole")
	assert.Equal(t, int64(5), updated[0].ResourceMutationInfo.Index, "protecting should preserve the resource's provenance")
	assert.NotContains(t, updated[0].PathMutationMap, api.ResolvedPath(""), "no path entry should be made for the resource")
	assert.False(t, updated[0].PathMutationMap["spec.replicas"].Protected, "path entries should be left as they are")

	updated, unresolved = SetProtection(setProtectionData(t), updated, deploymentResource(),
		map[api.ResolvedPath]bool{"": false, "spec.replicas": true}, testProvider)
	assert.Empty(t, unresolved)
	assert.False(t, updated[0].ResourceMutationInfo.Protected, "the empty path should re-open the resource as a whole")
	assert.True(t, updated[0].PathMutationMap["spec.replicas"].Protected, "a path can be protected in the same edit")
}

func TestSetProtectionWholeResourceTheUnitDeleted(t *testing.T) {
	// The Unit deleted the Service, so it is not in the data, but its record says so; protecting
	// it as a whole is what keeps a merge from adding it back.
	mutations := append(deploymentMutations(), api.ResourceMutation{
		Resource:             api.ResourceInfo{ResourceType: "v1/Service", ResourceName: "default/svc", ResourceNameWithoutScope: "svc"},
		ResourceMutationInfo: api.MutationInfo{MutationType: api.MutationTypeDelete, Index: 11},
	})
	updated, unresolved := SetProtection(setProtectionData(t), mutations, mutations[1].Resource,
		map[api.ResolvedPath]bool{"": true}, testProvider)
	assert.Empty(t, unresolved)
	assert.True(t, updated[1].ResourceMutationInfo.Protected)
}

func TestPatchMutationsLeavesAResourceProtectedAsAWholeAlone(t *testing.T) {
	// The target added a ServiceAccount of its own; then upstream added one by the same name.
	const targetYAML = `apiVersion: v1
kind: ServiceAccount
metadata:
  name: app
  namespace: default
  annotations:
    owner: target
`
	const upstreamYAML = `apiVersion: v1
kind: ServiceAccount
metadata:
  name: app
  namespace: default
  annotations:
    owner: upstream
`
	parse := func(data string) gaby.Container {
		docs, err := gaby.ParseAll([]byte(data))
		require.NoError(t, err)
		return docs
	}
	targetMutations, err := ComputeMutations(gaby.Container{}, parse(targetYAML), 3, testProvider)
	require.NoError(t, err)
	require.Len(t, targetMutations, 1)
	require.Empty(t, targetMutations[0].PathMutationMap, "a resource added whole has only a resource-level entry")
	patch, err := ComputeMutations(gaby.Container{}, parse(upstreamYAML), 1, testProvider)
	require.NoError(t, err)

	unprotected, _, err := PatchMutations(parse(targetYAML), targetMutations, patch, nil, testProvider, nil)
	require.NoError(t, err)
	assert.Contains(t, unprotected.String(), "owner: upstream", "unprotected, the upstream resource replaces the target's")

	protectedMutations, unresolved := SetProtection(parse(targetYAML), targetMutations, targetMutations[0].Resource,
		map[api.ResolvedPath]bool{"": true}, testProvider)
	require.Empty(t, unresolved)
	result, conflicts, err := PatchMutations(parse(targetYAML), protectedMutations, patch, nil, testProvider, nil)
	require.NoError(t, err)
	assert.Contains(t, result.String(), "owner: target", "protected as a whole, the target's resource is left alone")
	require.Len(t, conflicts, 1)
	assert.Equal(t, api.ConflictReasonProtectedPath, conflicts[0].Reason)
}
