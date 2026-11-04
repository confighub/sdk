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

// A source that removes an element of a list removes the element, whatever the target has
// protected inside it. What would be left of an element without its unprotected fields -- an
// ExternalSecret data entry with no secretKey, an env var with no name, a container with no
// image -- is not a smaller version of the element but a malformed one, and it is what the
// list's consumer reads. These tests pin that down for a positional list and a merge-keyed one,
// and for the ways the target can still keep the element: protecting the element itself, or a
// guard under it the operation is not cleared for.

const elementDeleteBase = `apiVersion: external-secrets.io/v1
kind: ExternalSecret
metadata:
  name: app-env
  namespace: default
spec:
  data:
  - secretKey: OLD_KEY
    remoteRef:
      key: /placeholder/env/OLD_KEY
  - secretKey: SIGNING_KEY
    remoteRef:
      key: /placeholder/env/SIGNING_KEY
`

// The source dropped OLD_KEY.
const elementDeleteSource = `apiVersion: external-secrets.io/v1
kind: ExternalSecret
metadata:
  name: app-env
  namespace: default
spec:
  data:
  - secretKey: SIGNING_KEY
    remoteRef:
      key: /placeholder/env/SIGNING_KEY
`

// The target pointed both entries at its own parameter store paths.
const elementDeleteTarget = `apiVersion: external-secrets.io/v1
kind: ExternalSecret
metadata:
  name: app-env
  namespace: default
spec:
  data:
  - secretKey: OLD_KEY
    remoteRef:
      key: /prod/env/OLD_KEY
  - secretKey: SIGNING_KEY
    remoteRef:
      key: /prod/env/SIGNING_KEY
`

var externalSecretResource = api.ResourceInfo{
	ResourceType:             "external-secrets.io/v1/ExternalSecret",
	ResourceName:             "default/app-env",
	ResourceNameWithoutScope: "app-env",
	ResourceCategory:         "Kubernetes",
}

func elementDeleteInputs(t *testing.T, base, source, target string) (gaby.Container, api.ResourceMutationList) {
	t.Helper()
	provider := k8skit.NewK8sResourceProvider()
	baseP, err := gaby.ParseAll([]byte(base))
	require.NoError(t, err)
	srcP, err := gaby.ParseAll([]byte(source))
	require.NoError(t, err)
	tgtP, err := gaby.ParseAll([]byte(target))
	require.NoError(t, err)
	patch, err := yamlkit.ComputeMutations(baseP, srcP, 1, provider)
	require.NoError(t, err)
	return tgtP, patch
}

func protectedPaths(resource api.ResourceInfo, paths map[string]string) api.ResourceMutationList {
	pathMutationMap := api.MutationMap{}
	for path, value := range paths {
		pathMutationMap[api.ResolvedPath(path)] = api.MutationInfo{
			MutationType: api.MutationTypeUpdate,
			Protected:    true,
			Value:        value,
		}
	}
	return api.ResourceMutationList{{
		Resource:             resource,
		ResourceMutationInfo: api.MutationInfo{MutationType: api.MutationTypeUpdate},
		PathMutationMap:      pathMutationMap,
	}}
}

// The case that emptied a production ExternalSecret: the target protects the parameter path of
// the entry the source removes, recorded by position. The entry goes whole, and the protected
// value it took with it is reported.
func TestPatch_DeletedPositionalElementGoesWholeDespiteProtectedLeaf(t *testing.T) {
	provider := k8skit.NewK8sResourceProvider()
	tgtP, patch := elementDeleteInputs(t, elementDeleteBase, elementDeleteSource, elementDeleteTarget)
	protection := protectedPaths(externalSecretResource, map[string]string{
		"spec.data.0.remoteRef.key": "/prod/env/OLD_KEY\n",
	})

	patched, conflicts, err := yamlkit.PatchMutations(tgtP, protection, patch, nil, provider, nil)
	require.NoError(t, err)

	got := string(patched.Bytes())
	assert.NotContains(t, got, "OLD_KEY", "the removed entry has to go whole")
	assert.NotContains(t, got, "{}", "no empty entry may be left in its place")
	assert.Contains(t, got, "key: /prod/env/SIGNING_KEY", "the entry the source kept is untouched")
	parsed, err := gaby.ParseAll([]byte(got))
	require.NoError(t, err)
	data := parsed[0].Path("spec.data")
	require.NotNil(t, data)
	assert.Len(t, data.Children(), 1)

	c := findConflict(t, conflicts, "app-env", "spec.data.0.remoteRef.key")
	assert.Equal(t, api.ConflictReasonDeleteShadowed, c.Reason)
	require.NotNil(t, c.Target)
	assert.True(t, c.Target.Protected)
}

const mergeKeyedDeleteBase = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: app
  namespace: default
spec:
  template:
    spec:
      containers:
      - name: app
        image: app:1
        env:
        - name: OLD_FLAG
          value: "off"
        - name: LOG_LEVEL
          value: info
`

const mergeKeyedDeleteSource = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: app
  namespace: default
spec:
  template:
    spec:
      containers:
      - name: app
        image: app:1
        env:
        - name: LOG_LEVEL
          value: info
`

const mergeKeyedDeleteTarget = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: app
  namespace: default
spec:
  template:
    spec:
      containers:
      - name: app
        image: app:1
        env:
        - name: OLD_FLAG
          value: "on"
        - name: LOG_LEVEL
          value: debug
`

var deploymentResource = api.ResourceInfo{
	ResourceType:             "apps/v1/Deployment",
	ResourceName:             "default/app",
	ResourceNameWithoutScope: "app",
	ResourceCategory:         "Kubernetes",
}

// The same for a merge-keyed list: the removed env var goes whole, not down to its name.
func TestPatch_DeletedMergeKeyedElementGoesWholeDespiteProtectedLeaf(t *testing.T) {
	provider := k8skit.NewK8sResourceProvider()
	tgtP, patch := elementDeleteInputs(t, mergeKeyedDeleteBase, mergeKeyedDeleteSource, mergeKeyedDeleteTarget)
	protection := protectedPaths(deploymentResource, map[string]string{
		"spec.template.spec.containers.?name=app.env.?name=OLD_FLAG.value":  "\"on\"\n",
		"spec.template.spec.containers.?name=app.env.?name=LOG_LEVEL.value": "debug\n",
	})

	patched, conflicts, err := yamlkit.PatchMutations(tgtP, protection, patch, nil, provider, nil)
	require.NoError(t, err)

	got := string(patched.Bytes())
	assert.NotContains(t, got, "OLD_FLAG", "the removed env var has to go whole")
	assert.NotContains(t, got, `value: "on"`)
	assert.Contains(t, got, "value: debug", "a protected value outside the removed element stays")

	c := findConflict(t, conflicts, "app", "spec.template.spec.containers.?name=app.env.?name=OLD_FLAG.value")
	assert.Equal(t, api.ConflictReasonDeleteShadowed, c.Reason)
}

// Protecting the element itself is how a target keeps an element the source removes, and it
// keeps all of it.
func TestPatch_ProtectedElementSurvivesItsDeletion(t *testing.T) {
	provider := k8skit.NewK8sResourceProvider()
	tgtP, patch := elementDeleteInputs(t, mergeKeyedDeleteBase, mergeKeyedDeleteSource, mergeKeyedDeleteTarget)
	protection := protectedPaths(deploymentResource, map[string]string{
		"spec.template.spec.containers.?name=app.env.?name=OLD_FLAG": "name: OLD_FLAG\nvalue: \"on\"\n",
	})

	patched, conflicts, err := yamlkit.PatchMutations(tgtP, protection, patch, nil, provider, nil)
	require.NoError(t, err)

	got := string(patched.Bytes())
	assert.Contains(t, got, "name: OLD_FLAG")
	assert.Contains(t, got, `value: "on"`)
	for _, c := range conflicts {
		assert.NotEqual(t, api.ConflictReasonDeleteShadowed, c.Reason,
			"nothing was displaced: %s", c.Path)
	}
}

// A guard under the element is a rule the operation is not cleared to break, so it keeps the
// element -- all of it, since keeping only the guarded field would leave the same broken entry.
func TestPatch_GuardedLeafWithholdsTheWholeElementDelete(t *testing.T) {
	provider := k8skit.NewK8sResourceProvider()
	tgtP, patch := elementDeleteInputs(t, mergeKeyedDeleteBase, mergeKeyedDeleteSource, mergeKeyedDeleteTarget)
	guards := &yamlkit.GuardFilter{
		Annotations: guardTableForApp(map[api.ResolvedPath]map[string]string{
			"spec.template.spec.containers.?name=app.env.?name=OLD_FLAG.value": {"owner": "platform-team"},
		}),
	}

	patched, conflicts, err := yamlkit.PatchMutationsGuarded(tgtP, nil, patch, nil, guards, provider, nil)
	require.NoError(t, err)

	got := string(patched.Bytes())
	assert.Contains(t, got, "name: OLD_FLAG", "the guarded env var is kept whole")
	assert.Contains(t, got, `value: "on"`)

	require.Len(t, conflicts, 1, "the withheld delete is reported once")
	c := conflicts[0]
	assert.Equal(t, api.ConflictReasonGuarded, c.Reason)
	assert.Equal(t, api.ResolvedPath("spec.template.spec.containers.?name=app.env.?name=OLD_FLAG"),
		yamlkit.CanonicalMutationPath(c.Path))
	require.NotNil(t, c.Guard)
	assert.Equal(t, "owner", c.Guard.Key)
}

// Cleared for the guard, the same Delete goes through whole.
func TestPatch_ClearedGuardLetsTheWholeElementGo(t *testing.T) {
	provider := k8skit.NewK8sResourceProvider()
	tgtP, patch := elementDeleteInputs(t, mergeKeyedDeleteBase, mergeKeyedDeleteSource, mergeKeyedDeleteTarget)
	guards := &yamlkit.GuardFilter{
		Annotations: guardTableForApp(map[api.ResolvedPath]map[string]string{
			"spec.template.spec.containers.?name=app.env.?name=OLD_FLAG.value": {"owner": "platform-team"},
		}),
		Clearance: api.Clearance{{
			Key: "owner", Operator: api.ClearanceOperatorIn, Values: []string{"platform-team"},
		}},
	}

	patched, conflicts, err := yamlkit.PatchMutationsGuarded(tgtP, nil, patch, nil, guards, provider, nil)
	require.NoError(t, err)

	got := string(patched.Bytes())
	assert.NotContains(t, got, "OLD_FLAG")
	assert.NotContains(t, got, `value: "on"`)
	assert.Empty(t, conflicts)
}

const nestedMappingDeleteBase = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: app
  namespace: default
spec:
  strategy:
    type: RollingUpdate
    rollingUpdate:
      maxSurge: 1
      maxUnavailable: 0
`

const nestedMappingDeleteSource = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: app
  namespace: default
spec: {}
`

const nestedMappingDeleteTarget = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: app
  namespace: default
spec:
  strategy:
    type: RollingUpdate
    rollingUpdate:
      maxSurge: 3
      maxUnavailable: 0
`

// A mapping is not an element: a protected key under a removed mapping survives it however deep
// it sits, not only when it is a direct child of the mapping removed.
func TestPatch_CoarseDeleteSparesProtectedGrandchild(t *testing.T) {
	provider := k8skit.NewK8sResourceProvider()
	tgtP, patch := elementDeleteInputs(t, nestedMappingDeleteBase, nestedMappingDeleteSource, nestedMappingDeleteTarget)
	protection := protectedPaths(deploymentResource, map[string]string{
		"spec.strategy.rollingUpdate.maxSurge": "3\n",
	})

	patched, conflicts, err := yamlkit.PatchMutations(tgtP, protection, patch, nil, provider, nil)
	require.NoError(t, err)

	got := string(patched.Bytes())
	assert.Contains(t, got, "maxSurge: 3", "the protected key two levels down must survive")
	assert.NotContains(t, got, "maxUnavailable", "the unprotected keys were the source's to remove")
	assert.NotContains(t, got, "type: RollingUpdate")

	c := findConflict(t, conflicts, "app", "spec.strategy.rollingUpdate.maxSurge")
	assert.Equal(t, api.ConflictReasonProtectedPath, c.Reason)
}

// The source dropped the whole list.
const listDeleteSource = `apiVersion: external-secrets.io/v1
kind: ExternalSecret
metadata:
  name: app-env
  namespace: default
spec: {}
`

// Removing the whole list is one Delete at the list, and it is decided the way the Delete of
// one of its elements is: the list goes, and the protected value it took with it is reported.
func TestPatch_DeletedListGoesWholeDespiteProtectedLeaf(t *testing.T) {
	provider := k8skit.NewK8sResourceProvider()
	tgtP, patch := elementDeleteInputs(t, elementDeleteBase, listDeleteSource, elementDeleteTarget)
	protection := protectedPaths(externalSecretResource, map[string]string{
		"spec.data.0.remoteRef.key": "/prod/env/OLD_KEY\n",
	})

	patched, conflicts, err := yamlkit.PatchMutations(tgtP, protection, patch, nil, provider, nil)
	require.NoError(t, err)

	got := string(patched.Bytes())
	assert.NotContains(t, got, "OLD_KEY", "the removed list has to go whole")
	assert.NotContains(t, got, "SIGNING_KEY")
	assert.Contains(t, got, "spec: {}")

	c := findConflict(t, conflicts, "app-env", "spec.data.0.remoteRef.key")
	assert.Equal(t, api.ConflictReasonDeleteShadowed, c.Reason)
}

const mergeKeyedListDeleteSource = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: app
  namespace: default
spec:
  template:
    spec:
      containers:
      - name: app
        image: app:1
`

// A guard inside the removed list keeps the whole list, not the guarded element alone.
func TestPatch_GuardedLeafWithholdsTheWholeListDelete(t *testing.T) {
	provider := k8skit.NewK8sResourceProvider()
	tgtP, patch := elementDeleteInputs(t, mergeKeyedDeleteBase, mergeKeyedListDeleteSource, mergeKeyedDeleteTarget)
	guards := &yamlkit.GuardFilter{
		Annotations: guardTableForApp(map[api.ResolvedPath]map[string]string{
			"spec.template.spec.containers.?name=app.env.?name=OLD_FLAG.value": {"owner": "platform-team"},
		}),
	}

	patched, conflicts, err := yamlkit.PatchMutationsGuarded(tgtP, nil, patch, nil, guards, provider, nil)
	require.NoError(t, err)

	got := string(patched.Bytes())
	assert.Contains(t, got, "name: OLD_FLAG")
	assert.Contains(t, got, "name: LOG_LEVEL", "the unguarded element stays with the list")

	require.Len(t, conflicts, 1, "the withheld delete is reported once")
	assert.Equal(t, api.ConflictReasonGuarded, conflicts[0].Reason)
	assert.Equal(t, api.ResolvedPath("spec.template.spec.containers.?name=app.env"),
		yamlkit.CanonicalMutationPath(conflicts[0].Path))
}
