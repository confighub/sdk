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

// A resource's namespace is part of its identity when it tells two resources of one type and
// name apart, and not when it changed: a variant fills in its base's placeholder namespace, and
// set-namespace moves a resource, without making it another resource.

const twoNamespaces = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: app
  namespace: blue
spec:
  replicas: 1
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: app
  namespace: green
spec:
  replicas: 2
`

func appIn(namespace string) api.ResourceInfo {
	return api.ResourceInfo{
		ResourceType:             "apps/v1/Deployment",
		ResourceName:             api.ResourceName(namespace + "/app"),
		ResourceNameWithoutScope: "app",
		ResourceCategory:         "Resource",
	}
}

func replicasPatch(namespace, replicas string) api.ResourceMutation {
	return api.ResourceMutation{
		Resource:             appIn(namespace),
		ResourceMutationInfo: api.MutationInfo{MutationType: api.MutationTypeUpdate, Index: 2},
		PathMutationMap: api.MutationMap{
			"spec.replicas": api.MutationInfo{MutationType: api.MutationTypeUpdate, Index: 2, Value: replicas},
		},
	}
}

func replicasOf(t *testing.T, docs gaby.Container, namespace string) int {
	t.Helper()
	for _, doc := range docs {
		if doc.Path("metadata.namespace").Data() == namespace {
			replicas, _, err := yamlkit.YamlSafePathGetValue[int](doc, "spec.replicas", false)
			require.NoError(t, err)
			return replicas
		}
	}
	t.Fatalf("no Deployment in namespace %s", namespace)
	return 0
}

// Protection recorded for the Deployment in one namespace holds that Deployment's path, not the
// same path of the Deployment of the same name in another namespace.
func TestPatch_ProtectionStaysWithItsNamespace(t *testing.T) {
	provider := k8skit.NewK8sResourceProvider()
	parsed, err := gaby.ParseAll([]byte(twoNamespaces))
	require.NoError(t, err)
	protection := api.ResourceMutationList{{
		Resource:             appIn("blue"),
		ResourceMutationInfo: api.MutationInfo{MutationType: api.MutationTypeUpdate},
		PathMutationMap:      api.MutationMap{"spec.replicas": api.MutationInfo{Protected: true}},
	}}
	patch := api.ResourceMutationList{replicasPatch("green", "5\n"), replicasPatch("blue", "5\n")}

	patched, conflicts, err := yamlkit.PatchMutations(parsed, protection, patch, nil, provider, nil)
	require.NoError(t, err)
	assert.Equal(t, 1, replicasOf(t, patched, "blue"), "blue's replicas are protected")
	assert.Equal(t, 5, replicasOf(t, patched, "green"), "green's are not")
	require.Len(t, conflicts, 1)
	assert.Equal(t, api.ResourceName("blue/app"), conflicts[0].Resource.ResourceName)
}

// A patch recorded under the namespace a resource had upstream applies to it in its own.
func TestPatch_PatchRecordedInAnotherNamespace(t *testing.T) {
	provider := k8skit.NewK8sResourceProvider()
	parsed, err := gaby.ParseAll([]byte(conflictTestBase))
	require.NoError(t, err)
	patch := api.ResourceMutationList{replicasPatch("confighubplaceholder", "5\n")}

	patched, conflicts, err := yamlkit.PatchMutations(parsed, nil, patch, nil, provider, nil)
	require.NoError(t, err)
	assert.Empty(t, conflicts)
	assert.Equal(t, 5, replicasOf(t, patched, "default"))
}

// Diffing pairs each Deployment with the one in its own namespace, whatever order they are in.
func TestComputeMutations_PairsByNamespaceFirst(t *testing.T) {
	provider := k8skit.NewK8sResourceProvider()
	previous, err := gaby.ParseAll([]byte(twoNamespaces))
	require.NoError(t, err)
	modified, err := gaby.ParseAll([]byte(`apiVersion: apps/v1
kind: Deployment
metadata:
  name: app
  namespace: green
spec:
  replicas: 2
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: app
  namespace: blue
spec:
  replicas: 3
`))
	require.NoError(t, err)

	mutations, err := yamlkit.ComputeMutations(previous, modified, 1, provider)
	require.NoError(t, err)
	changed := map[api.ResourceName]api.MutationMap{}
	for _, mutation := range mutations {
		if mutation.ResourceMutationInfo.MutationType != api.MutationTypeNone {
			changed[mutation.Resource.ResourceName] = mutation.PathMutationMap
		}
	}
	require.Len(t, changed, 1, "only blue changed")
	assert.Contains(t, changed["blue/app"], api.ResolvedPath("spec.replicas"))
}

// A Deployment moved to another namespace is the same Deployment, updated, not one deleted and
// another added.
func TestComputeMutations_NamespaceChangeIsAnUpdate(t *testing.T) {
	provider := k8skit.NewK8sResourceProvider()
	previous, err := gaby.ParseAll([]byte(conflictTestBase))
	require.NoError(t, err)
	modified, err := gaby.ParseAll([]byte(`apiVersion: apps/v1
kind: Deployment
metadata:
  name: app
  namespace: prod
spec:
  replicas: 1
  template:
    spec:
      containers:
      - name: app
        image: nginx:1.19
`))
	require.NoError(t, err)

	mutations, err := yamlkit.ComputeMutations(previous, modified, 1, provider)
	require.NoError(t, err)
	require.Len(t, mutations, 1)
	assert.Equal(t, api.MutationTypeUpdate, mutations[0].ResourceMutationInfo.MutationType)
	assert.Contains(t, mutations[0].PathMutationMap, api.ResolvedPath("metadata.namespace"))
}

// The document for a stored entry is the one in the entry's namespace, or, when the entry's
// namespace is no longer there, the only document of the name.
func TestFindResourceDoc_NamespaceFirst(t *testing.T) {
	provider := k8skit.NewK8sResourceProvider()
	parsed, err := gaby.ParseAll([]byte(twoNamespaces))
	require.NoError(t, err)

	target := appIn("green")
	doc, info := yamlkit.FindResourceDoc(parsed, provider, &target)
	require.NotNil(t, doc)
	assert.Equal(t, api.ResourceName("green/app"), info.ResourceName)

	moved := appIn("confighubplaceholder")
	doc, _ = yamlkit.FindResourceDoc(parsed, provider, &moved)
	assert.Nil(t, doc, "two Deployments named app, and neither in confighubplaceholder")

	single, err := gaby.ParseAll([]byte(conflictTestBase))
	require.NoError(t, err)
	doc, info = yamlkit.FindResourceDoc(single, provider, &moved)
	require.NotNil(t, doc)
	assert.Equal(t, api.ResourceName("default/app"), info.ResourceName)
}
