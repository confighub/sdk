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
