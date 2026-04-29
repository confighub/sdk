// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package k8skit_test

import (
	"testing"

	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/third_party/gaby"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Tests for element-level rename detection inside merge-keyed arrays.
// ComputeMutations pairs unmatched-modified and unmatched-previous elements
// by similarity (path-mutation-count / element line count). When paired:
//
//   - child path mutations are emitted under the previous merge-key value so
//     that SubtractMutations aligns with target-side diffs (which still see
//     the previous key);
//   - the rename is recorded in ArrayElementAliases on the resource;
//   - PatchMutations rewrites the element's merge-key field at apply time,
//     before the reorder pass.
//
// Verified scenarios: simple rename, rename + child Update, rename with
// target-side override on a child path, rename ignored when elements are too
// dissimilar, multiple renames in the same array.

const baseInitContainersV1 = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: app
  namespace: default
  annotations:
    confighub.com/ResourceMergeID: 22222222-2222-2222-2222-222222222222
spec:
  replicas: 1
  selector:
    matchLabels:
      app: app
  template:
    spec:
      initContainers:
      - name: db-init
        image: ghcr.io/example/app:v1
        env:
        - name: ENV_A
          value: alpha
        - name: ENV_B
          value: bravo
      - name: db-migrate
        image: ghcr.io/example/app:v1
        env:
        - name: ENV_A
          value: alpha
      containers:
      - name: app
        image: ghcr.io/example/app:v1
`

// TestElementRename_DetectedAndAppliedAsUpdate — source renames db-init to
// db-init-v2 with no other changes. ComputeMutations should classify it as
// a rename: no Add/Delete pair in PathMutationMap, instead an
// ArrayElementAliases entry with previous-key db-init -> new-key db-init-v2.
// PatchMutations rewrites the element's merge-key field at apply time.
func TestElementRename_DetectedAndAppliedAsUpdate(t *testing.T) {
	source := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: app
  namespace: default
  annotations:
    confighub.com/ResourceMergeID: 22222222-2222-2222-2222-222222222222
spec:
  replicas: 1
  selector:
    matchLabels:
      app: app
  template:
    spec:
      initContainers:
      - name: db-init-v2
        image: ghcr.io/example/app:v1
        env:
        - name: ENV_A
          value: alpha
        - name: ENV_B
          value: bravo
      - name: db-migrate
        image: ghcr.io/example/app:v1
        env:
        - name: ENV_A
          value: alpha
      containers:
      - name: app
        image: ghcr.io/example/app:v1
`
	mutations := computeMutationsHelper(t, baseInitContainersV1, source)
	require.Len(t, mutations, 1)
	m := mutations[0]
	arrayPath := api.ResolvedPath("spec.template.spec.initContainers")
	require.Contains(t, m.ArrayElementAliases, arrayPath, "rename should be recorded")
	assert.Equal(t, "db-init-v2", m.ArrayElementAliases[arrayPath]["db-init"])

	// No Add/Delete pair in path mutations for the renamed element.
	for path, info := range m.PathMutationMap {
		assert.NotEqual(t, api.MutationTypeDelete, info.MutationType,
			"unexpected Delete on path %s", path)
	}

	// Apply to the unchanged target — element's merge-key field gets
	// rewritten to the new key.
	out := runUpgrade(t, baseInitContainersV1, source, baseInitContainersV1)
	docs, err := gaby.ParseAll(out)
	require.NoError(t, err)
	var names []string
	for _, c := range docs[0].S("spec", "template", "spec", "initContainers").Children() {
		names = append(names, c.S("name").Data().(string))
	}
	assert.Equal(t, []string{"db-init-v2", "db-migrate"}, names)
}

// TestElementRename_WithChildUpdate — the renamed element also has a field
// change (image bump). The path mutation is recorded under the previous key
// so SubtractMutations aligns with target-side overrides.
func TestElementRename_WithChildUpdate(t *testing.T) {
	source := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: app
  namespace: default
  annotations:
    confighub.com/ResourceMergeID: 22222222-2222-2222-2222-222222222222
spec:
  replicas: 1
  selector:
    matchLabels:
      app: app
  template:
    spec:
      initContainers:
      - name: db-init-v2
        image: ghcr.io/example/app:v2
        env:
        - name: ENV_A
          value: alpha
        - name: ENV_B
          value: bravo
      - name: db-migrate
        image: ghcr.io/example/app:v1
        env:
        - name: ENV_A
          value: alpha
      containers:
      - name: app
        image: ghcr.io/example/app:v1
`
	mutations := computeMutationsHelper(t, baseInitContainersV1, source)
	require.Len(t, mutations, 1)
	m := mutations[0]
	arrayPath := api.ResolvedPath("spec.template.spec.initContainers")
	require.Contains(t, m.ArrayElementAliases, arrayPath)
	assert.Equal(t, "db-init-v2", m.ArrayElementAliases[arrayPath]["db-init"])

	// Path mutation for the image bump is recorded under the PREVIOUS key.
	imagePath := api.ResolvedPath("spec.template.spec.initContainers.?name=db-init;@0.image")
	require.Contains(t, m.PathMutationMap, imagePath, "image change uses previous key")
	assert.Equal(t, api.MutationTypeUpdate, m.PathMutationMap[imagePath].MutationType)

	out := runUpgrade(t, baseInitContainersV1, source, baseInitContainersV1)
	docs, err := gaby.ParseAll(out)
	require.NoError(t, err)
	containers := docs[0].S("spec", "template", "spec", "initContainers").Children()
	require.Equal(t, "db-init-v2", containers[0].S("name").Data().(string))
	assert.Equal(t, "ghcr.io/example/app:v2", containers[0].S("image").Data().(string))
}

// TestElementRename_TargetOverrideSurvives — source renames db-init -> db-init-v2
// and bumps its image. Target had its own override on db-init's image. Because
// the rename emits the image path under the previous key (db-init),
// SubtractMutations cancels source's image change against target's image
// change. The rename still applies (the element's merge-key field is
// rewritten) and target's image override survives the patch.
func TestElementRename_TargetOverrideSurvives(t *testing.T) {
	source := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: app
  namespace: default
  annotations:
    confighub.com/ResourceMergeID: 22222222-2222-2222-2222-222222222222
spec:
  replicas: 1
  selector:
    matchLabels:
      app: app
  template:
    spec:
      initContainers:
      - name: db-init-v2
        image: ghcr.io/example/app:v2
        env:
        - name: ENV_A
          value: alpha
        - name: ENV_B
          value: bravo
      - name: db-migrate
        image: ghcr.io/example/app:v1
        env:
        - name: ENV_A
          value: alpha
      containers:
      - name: app
        image: ghcr.io/example/app:v1
`
	target := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: app
  namespace: default
  annotations:
    confighub.com/ResourceMergeID: 22222222-2222-2222-2222-222222222222
spec:
  replicas: 1
  selector:
    matchLabels:
      app: app
  template:
    spec:
      initContainers:
      - name: db-init
        image: ghcr.io/example/app:v3
        env:
        - name: ENV_A
          value: alpha
        - name: ENV_B
          value: bravo
      - name: db-migrate
        image: ghcr.io/example/app:v1
        env:
        - name: ENV_A
          value: alpha
      containers:
      - name: app
        image: ghcr.io/example/app:v1
`
	out := runUpgrade(t, baseInitContainersV1, source, target)
	docs, err := gaby.ParseAll(out)
	require.NoError(t, err)
	containers := docs[0].S("spec", "template", "spec", "initContainers").Children()
	require.Equal(t, "db-init-v2", containers[0].S("name").Data().(string),
		"rename applied — element's merge-key field rewritten")
	assert.Equal(t, "ghcr.io/example/app:v3", containers[0].S("image").Data().(string),
		"target's image override survives — source's image change subtracted")
}

// TestElementRename_MultipleRenamesInSameArray — both initContainers are
// renamed (different new names, similar bodies). Each rename is recorded.
func TestElementRename_MultipleRenamesInSameArray(t *testing.T) {
	source := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: app
  namespace: default
  annotations:
    confighub.com/ResourceMergeID: 22222222-2222-2222-2222-222222222222
spec:
  replicas: 1
  selector:
    matchLabels:
      app: app
  template:
    spec:
      initContainers:
      - name: db-init-renamed
        image: ghcr.io/example/app:v1
        env:
        - name: ENV_A
          value: alpha
        - name: ENV_B
          value: bravo
      - name: db-migrate-renamed
        image: ghcr.io/example/app:v1
        env:
        - name: ENV_A
          value: alpha
      containers:
      - name: app
        image: ghcr.io/example/app:v1
`
	mutations := computeMutationsHelper(t, baseInitContainersV1, source)
	require.Len(t, mutations, 1)
	arrayPath := api.ResolvedPath("spec.template.spec.initContainers")
	aliases := mutations[0].ArrayElementAliases[arrayPath]
	require.NotNil(t, aliases)
	assert.Equal(t, "db-init-renamed", aliases["db-init"])
	assert.Equal(t, "db-migrate-renamed", aliases["db-migrate"])

	out := runUpgrade(t, baseInitContainersV1, source, baseInitContainersV1)
	docs, err := gaby.ParseAll(out)
	require.NoError(t, err)
	var names []string
	for _, c := range docs[0].S("spec", "template", "spec", "initContainers").Children() {
		names = append(names, c.S("name").Data().(string))
	}
	assert.Equal(t, []string{"db-init-renamed", "db-migrate-renamed"}, names)
}

// TestElementRename_InsertPlusUnrelatedRemoveIsNotARename — source removes
// db-init AND inserts a new db-bootstrap with substantially different fields.
// Even though both ends have one unmatched element of the same kind, the
// rename heuristic (score < 0.3) rejects the pair: db-bootstrap has too
// many field-level differences from db-init for the change to be confused
// with a rename. Falls back to Add + Delete with the looser-Delete behavior.
func TestElementRename_InsertPlusUnrelatedRemoveIsNotARename(t *testing.T) {
	source := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: app
  namespace: default
  annotations:
    confighub.com/ResourceMergeID: 22222222-2222-2222-2222-222222222222
spec:
  replicas: 1
  selector:
    matchLabels:
      app: app
  template:
    spec:
      initContainers:
      - name: db-bootstrap
        image: ghcr.io/different/bootstrap:v1
        command: ["/bin/bootstrap"]
        args: ["--init-mode=full", "--seed-only"]
        envFrom:
        - configMapRef:
            name: bootstrap-config
        resources:
          requests:
            cpu: 50m
            memory: 64Mi
      - name: db-migrate
        image: ghcr.io/example/app:v1
        env:
        - name: ENV_A
          value: alpha
      containers:
      - name: app
        image: ghcr.io/example/app:v1
`
	mutations := computeMutationsHelper(t, baseInitContainersV1, source)
	require.Len(t, mutations, 1)
	m := mutations[0]
	arrayPath := api.ResolvedPath("spec.template.spec.initContainers")
	assert.NotContains(t, m.ArrayElementAliases, arrayPath,
		"db-bootstrap is too dissimilar from db-init to pair as a rename")

	// We expect a Delete for the gone element and an Add for the new one.
	deletePath := api.ResolvedPath("spec.template.spec.initContainers.?name=db-init;@0")
	addPath := api.ResolvedPath("spec.template.spec.initContainers.?name=db-bootstrap;@0")
	require.Contains(t, m.PathMutationMap, deletePath)
	assert.Equal(t, api.MutationTypeDelete, m.PathMutationMap[deletePath].MutationType)
	require.Contains(t, m.PathMutationMap, addPath)
	assert.Equal(t, api.MutationTypeAdd, m.PathMutationMap[addPath].MutationType)
}

// TestElementRename_CoarseSubtreeNotMistakenForRename — when a similar element
// is added alongside a removal whose body is mostly a wholly-different subtree
// (e.g., a different env block), the leaf-aware cost reflects how many leaves
// changed, not just how many top-level paths show up in the sub-diff. With
// len()-based cost the pair would have score = small (one .env Update entry
// hides many leaves); with leaf-aware cost it correctly exceeds the rename
// threshold and falls back to Add+Delete.
func TestElementRename_CoarseSubtreeNotMistakenForRename(t *testing.T) {
	// Source removes db-init and adds db-other with a totally different
	// env block (5 env vars, none in common with db-init). The two
	// elements share only their image and merge-key shape; a naive count
	// would see "name changed + env Update" = 2 paths and pair, but the
	// env Update's Value contains 10 leaves (5 env entries × 2 fields each),
	// pushing leaf-aware cost over the 0.3 threshold.
	source := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: app
  namespace: default
  annotations:
    confighub.com/ResourceMergeID: 22222222-2222-2222-2222-222222222222
spec:
  replicas: 1
  selector:
    matchLabels:
      app: app
  template:
    spec:
      initContainers:
      - name: db-other
        image: ghcr.io/example/app:v1
        env:
        - name: PG_HOST
          value: pg.local
        - name: PG_PORT
          value: "5432"
        - name: PG_USER
          value: app
        - name: PG_PASS
          value: secret
        - name: PG_DB
          value: appdb
      - name: db-migrate
        image: ghcr.io/example/app:v1
        env:
        - name: ENV_A
          value: alpha
      containers:
      - name: app
        image: ghcr.io/example/app:v1
`
	mutations := computeMutationsHelper(t, baseInitContainersV1, source)
	require.Len(t, mutations, 1)
	arrayPath := api.ResolvedPath("spec.template.spec.initContainers")
	assert.NotContains(t, mutations[0].ArrayElementAliases, arrayPath,
		"coarse subtree change should not be confused with a rename")
}

// TestElementRename_PreservesSourceArrayOrder — source renames AND reorders.
// The reorder pass uses ArrayOrders (which references new keys) and the
// rename pass rewrites merge-key fields before the reorder runs, so the new
// names land in source's desired positions.
func TestElementRename_PreservesSourceArrayOrder(t *testing.T) {
	source := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: app
  namespace: default
  annotations:
    confighub.com/ResourceMergeID: 22222222-2222-2222-2222-222222222222
spec:
  replicas: 1
  selector:
    matchLabels:
      app: app
  template:
    spec:
      initContainers:
      - name: db-migrate
        image: ghcr.io/example/app:v1
        env:
        - name: ENV_A
          value: alpha
      - name: db-init-v2
        image: ghcr.io/example/app:v1
        env:
        - name: ENV_A
          value: alpha
        - name: ENV_B
          value: bravo
      containers:
      - name: app
        image: ghcr.io/example/app:v1
`
	out := runUpgrade(t, baseInitContainersV1, source, baseInitContainersV1)
	docs, err := gaby.ParseAll(out)
	require.NoError(t, err)
	var names []string
	for _, c := range docs[0].S("spec", "template", "spec", "initContainers").Children() {
		names = append(names, c.S("name").Data().(string))
	}
	assert.Equal(t, []string{"db-migrate", "db-init-v2"}, names)
}
