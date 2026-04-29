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

// These tests cover the three-way merge upgrade scenarios for arrays with merge
// keys — initContainers (merge key: name) and the env var array nested inside
// each initContainer (merge key: name, but at a separate path level). They
// exercise change / addition / removal / rename / insertion of elements, both
// without target-side overrides (no subtraction effect) and with overrides
// (SubtractMutations preserves the target-side change).
//
// The subtraction is invoked via PatchMutations' mutationsToSubtract argument
// so the tests cover the same code path that the upgrade-unit flow drives in
// production.

// runUpgrade computes source mutations (base→sourceEnd) and target mutations
// (base→target), then applies the source patch to target via PatchMutations
// with target mutations as the subtract list. Returns the patched YAML.
func runUpgrade(t *testing.T, base, sourceEnd, target string) []byte {
	t.Helper()
	provider := k8skit.NewK8sResourceProvider()

	baseParsed, err := gaby.ParseAll([]byte(base))
	require.NoError(t, err)
	sourceEndParsed, err := gaby.ParseAll([]byte(sourceEnd))
	require.NoError(t, err)
	targetParsed, err := gaby.ParseAll([]byte(target))
	require.NoError(t, err)

	sourceMutations, err := yamlkit.ComputeMutations(baseParsed, sourceEndParsed, 1, provider)
	require.NoError(t, err)
	targetMutations, err := yamlkit.ComputeMutations(baseParsed, targetParsed, 2, provider)
	require.NoError(t, err)

	patched, _, err := yamlkit.PatchMutations(targetParsed, nil, sourceMutations, targetMutations, provider, nil)
	require.NoError(t, err)
	return patched.Bytes()
}

// initContainerNames returns the list of initContainer names from the patched
// document, in document order.
func initContainerNames(t *testing.T, yamlBytes []byte) []string {
	t.Helper()
	docs, err := gaby.ParseAll(yamlBytes)
	require.NoError(t, err)
	require.Len(t, docs, 1)
	children := docs[0].S("spec", "template", "spec", "initContainers").Children()
	names := make([]string, 0, len(children))
	for _, c := range children {
		if n := c.S("name"); n != nil {
			names = append(names, n.Data().(string))
		}
	}
	return names
}

// initContainerImage returns the image of the initContainer at the given index.
func initContainerImage(t *testing.T, yamlBytes []byte, idx int) string {
	t.Helper()
	docs, err := gaby.ParseAll(yamlBytes)
	require.NoError(t, err)
	children := docs[0].S("spec", "template", "spec", "initContainers").Children()
	require.Greater(t, len(children), idx)
	return children[idx].S("image").Data().(string)
}

// initContainerEnvNames returns the env-var names for the named initContainer.
func initContainerEnvNames(t *testing.T, yamlBytes []byte, containerName string) []string {
	t.Helper()
	docs, err := gaby.ParseAll(yamlBytes)
	require.NoError(t, err)
	for _, c := range docs[0].S("spec", "template", "spec", "initContainers").Children() {
		if n := c.S("name"); n != nil && n.Data().(string) == containerName {
			env := c.S("env")
			if env == nil {
				return nil
			}
			children := env.Children()
			names := make([]string, 0, len(children))
			for _, e := range children {
				if en := e.S("name"); en != nil {
					names = append(names, en.Data().(string))
				}
			}
			return names
		}
	}
	t.Fatalf("initContainer %q not found", containerName)
	return nil
}

// initContainerEnvValue returns the value of the named env var on the named
// initContainer.
func initContainerEnvValue(t *testing.T, yamlBytes []byte, containerName, envName string) string {
	t.Helper()
	docs, err := gaby.ParseAll(yamlBytes)
	require.NoError(t, err)
	for _, c := range docs[0].S("spec", "template", "spec", "initContainers").Children() {
		if n := c.S("name"); n != nil && n.Data().(string) == containerName {
			for _, e := range c.S("env").Children() {
				if en := e.S("name"); en != nil && en.Data().(string) == envName {
					return e.S("value").Data().(string)
				}
			}
		}
	}
	t.Fatalf("env var %q on initContainer %q not found", envName, containerName)
	return ""
}

// baseDeploymentTwoInits is the merge-base manifest used by every test in this
// file. It has two initContainers each with two env vars, so we have two
// merge-key-pathed arrays nested one inside the other.
const baseDeploymentTwoInits = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  namespace: confighubplaceholder
  annotations:
    confighub.com/ResourceMergeID: 11111111-1111-1111-1111-111111111111
spec:
  replicas: 1
  selector:
    matchLabels:
      app: myapp
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
        - name: ENV_B
          value: bravo
      containers:
      - name: app
        image: ghcr.io/example/app:v1
`

// targetSameAsBase is the most common downstream-side state: byte-identical to
// the merge base. Tests that want a target-side override use a separate string.
const targetSameAsBase = baseDeploymentTwoInits

// ------------------------------------------------------------------
// initContainer-level scenarios (merge key: name on initContainers)
// ------------------------------------------------------------------

func TestUpgrade_InitContainer_Change(t *testing.T) {
	// Source: bump db-init's image. db-migrate unchanged.
	source := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  namespace: confighubplaceholder
  annotations:
    confighub.com/ResourceMergeID: 11111111-1111-1111-1111-111111111111
spec:
  replicas: 1
  selector:
    matchLabels:
      app: myapp
  template:
    spec:
      initContainers:
      - name: db-init
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
        - name: ENV_B
          value: bravo
      containers:
      - name: app
        image: ghcr.io/example/app:v1
`

	t.Run("no override propagates change", func(t *testing.T) {
		out := runUpgrade(t, baseDeploymentTwoInits, source, targetSameAsBase)
		assert.Equal(t, []string{"db-init", "db-migrate"}, initContainerNames(t, out))
		assert.Equal(t, "ghcr.io/example/app:v2", initContainerImage(t, out, 0))
		assert.Equal(t, "ghcr.io/example/app:v1", initContainerImage(t, out, 1))
	})

	t.Run("target image override is preserved", func(t *testing.T) {
		// Target independently set db-init image to v3.
		target := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  namespace: confighubplaceholder
  annotations:
    confighub.com/ResourceMergeID: 11111111-1111-1111-1111-111111111111
spec:
  replicas: 1
  selector:
    matchLabels:
      app: myapp
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
        - name: ENV_B
          value: bravo
      containers:
      - name: app
        image: ghcr.io/example/app:v1
`
		out := runUpgrade(t, baseDeploymentTwoInits, source, target)
		assert.Equal(t, []string{"db-init", "db-migrate"}, initContainerNames(t, out))
		// Target's v3 wins over source's v2.
		assert.Equal(t, "ghcr.io/example/app:v3", initContainerImage(t, out, 0))
		assert.Equal(t, "ghcr.io/example/app:v1", initContainerImage(t, out, 1))
	})
}

func TestUpgrade_InitContainer_Addition(t *testing.T) {
	// Source: add a third initContainer at the end.
	source := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  namespace: confighubplaceholder
  annotations:
    confighub.com/ResourceMergeID: 11111111-1111-1111-1111-111111111111
spec:
  replicas: 1
  selector:
    matchLabels:
      app: myapp
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
        - name: ENV_B
          value: bravo
      - name: db-seed
        image: ghcr.io/example/app:v1
        env:
        - name: ENV_A
          value: alpha
      containers:
      - name: app
        image: ghcr.io/example/app:v1
`

	t.Run("no override appends new container", func(t *testing.T) {
		out := runUpgrade(t, baseDeploymentTwoInits, source, targetSameAsBase)
		assert.Equal(t, []string{"db-init", "db-migrate", "db-seed"}, initContainerNames(t, out))
		assert.Equal(t, []string{"ENV_A"}, initContainerEnvNames(t, out, "db-seed"))
	})

	t.Run("with target image override on existing container", func(t *testing.T) {
		// Target overrides db-init image; addition is independent and still applies.
		target := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  namespace: confighubplaceholder
  annotations:
    confighub.com/ResourceMergeID: 11111111-1111-1111-1111-111111111111
spec:
  replicas: 1
  selector:
    matchLabels:
      app: myapp
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
        - name: ENV_B
          value: bravo
      containers:
      - name: app
        image: ghcr.io/example/app:v1
`
		out := runUpgrade(t, baseDeploymentTwoInits, source, target)
		assert.Equal(t, []string{"db-init", "db-migrate", "db-seed"}, initContainerNames(t, out))
		assert.Equal(t, "ghcr.io/example/app:v3", initContainerImage(t, out, 0))
	})
}

func TestUpgrade_InitContainer_Removal(t *testing.T) {
	// Source: remove db-init, keeping db-migrate.
	source := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  namespace: confighubplaceholder
  annotations:
    confighub.com/ResourceMergeID: 11111111-1111-1111-1111-111111111111
spec:
  replicas: 1
  selector:
    matchLabels:
      app: myapp
  template:
    spec:
      initContainers:
      - name: db-migrate
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

	t.Run("no override removes container", func(t *testing.T) {
		out := runUpgrade(t, baseDeploymentTwoInits, source, targetSameAsBase)
		assert.Equal(t, []string{"db-migrate"}, initContainerNames(t, out))
	})

	t.Run("target override on the removed container is shadowed", func(t *testing.T) {
		// Target customized db-init's image. The source-side Delete passes
		// through (the upstream wants the container gone; once removed the
		// target's image override has nowhere to live). db-init is gone in the
		// result; the override is reported as a DeleteShadowed conflict to
		// the SubtractMutations caller, but runUpgrade discards that here.
		target := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  namespace: confighubplaceholder
  annotations:
    confighub.com/ResourceMergeID: 11111111-1111-1111-1111-111111111111
spec:
  replicas: 1
  selector:
    matchLabels:
      app: myapp
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
        - name: ENV_B
          value: bravo
      containers:
      - name: app
        image: ghcr.io/example/app:v1
`
		out := runUpgrade(t, baseDeploymentTwoInits, source, target)
		assert.Equal(t, []string{"db-migrate"}, initContainerNames(t, out))
	})
}

func TestUpgrade_InitContainer_Rename(t *testing.T) {
	// This is the original bug scenario: source renames db-init -> db-init-v2.
	// ComputeMutations emits this as Delete ?name=db-init;@0 + Add
	// ?name=db-init-v2;@0 (different merge keys, same parent index).
	source := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  namespace: confighubplaceholder
  annotations:
    confighub.com/ResourceMergeID: 11111111-1111-1111-1111-111111111111
spec:
  replicas: 1
  selector:
    matchLabels:
      app: myapp
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
        - name: ENV_B
          value: bravo
      containers:
      - name: app
        image: ghcr.io/example/app:v1
`

	t.Run("no override applies rename in source-side order", func(t *testing.T) {
		// Without subtraction the Delete survives; Deletes-before-Adds ordering
		// inside applyPathMutations means db-init is gone by the time the Add
		// runs. The Add appends, then the reorder pass uses the recorded
		// ArrayOrder to restore the source-side sequence: [db-init-v2, db-migrate].
		out := runUpgrade(t, baseDeploymentTwoInits, source, targetSameAsBase)
		assert.Equal(t, []string{"db-init-v2", "db-migrate"}, initContainerNames(t, out))
	})

	t.Run("override on renamed container is shadowed; rename completes", func(t *testing.T) {
		// Target customized db-init.image. The source-side Delete for db-init
		// passes through (its .image override can't survive the parent's
		// removal — that's reported as a DeleteShadowed conflict). The Add
		// for db-init-v2 then appends, since index 0 is now occupied by
		// db-migrate with a different merge-key value.
		target := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  namespace: confighubplaceholder
  annotations:
    confighub.com/ResourceMergeID: 11111111-1111-1111-1111-111111111111
spec:
  replicas: 1
  selector:
    matchLabels:
      app: myapp
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
        - name: ENV_B
          value: bravo
      containers:
      - name: app
        image: ghcr.io/example/app:v1
`
		out := runUpgrade(t, baseDeploymentTwoInits, source, target)
		// Reorder pass restores the source-side sequence: db-init-v2 before
		// db-migrate (the position db-init held before the rename).
		assert.Equal(t, []string{"db-init-v2", "db-migrate"}, initContainerNames(t, out))
	})
}

func TestUpgrade_InitContainer_Insertion(t *testing.T) {
	// Source: insert db-pre at the front. db-init shifts to index 1, db-migrate
	// to index 2.
	source := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  namespace: confighubplaceholder
  annotations:
    confighub.com/ResourceMergeID: 11111111-1111-1111-1111-111111111111
spec:
  replicas: 1
  selector:
    matchLabels:
      app: myapp
  template:
    spec:
      initContainers:
      - name: db-pre
        image: ghcr.io/example/app:v1
        env:
        - name: ENV_A
          value: alpha
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
        - name: ENV_B
          value: bravo
      containers:
      - name: app
        image: ghcr.io/example/app:v1
`

	t.Run("no override applies insertion at the source-side position", func(t *testing.T) {
		// Source inserts db-pre at index 0. PatchMutations applies the Add
		// (appended for now), then the reorder pass uses the recorded
		// ArrayOrder to put db-pre at the front to match source order.
		out := runUpgrade(t, baseDeploymentTwoInits, source, targetSameAsBase)
		assert.Equal(t, []string{"db-pre", "db-init", "db-migrate"}, initContainerNames(t, out))
	})

	t.Run("with override on shifted container", func(t *testing.T) {
		// Target customized db-init.image; source inserts db-pre at index 0.
		// db-init's customization survives; db-pre lands at the front per
		// the reorder pass.
		target := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  namespace: confighubplaceholder
  annotations:
    confighub.com/ResourceMergeID: 11111111-1111-1111-1111-111111111111
spec:
  replicas: 1
  selector:
    matchLabels:
      app: myapp
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
        - name: ENV_B
          value: bravo
      containers:
      - name: app
        image: ghcr.io/example/app:v1
`
		out := runUpgrade(t, baseDeploymentTwoInits, source, target)
		assert.Equal(t, []string{"db-pre", "db-init", "db-migrate"}, initContainerNames(t, out))
		// db-init customization survives.
		dbInitImage := ""
		docs, err := gaby.ParseAll(out)
		require.NoError(t, err)
		for _, c := range docs[0].S("spec", "template", "spec", "initContainers").Children() {
			if c.S("name").Data().(string) == "db-init" {
				dbInitImage = c.S("image").Data().(string)
			}
		}
		assert.Equal(t, "ghcr.io/example/app:v3", dbInitImage)
	})
}

// ------------------------------------------------------------------
// Reorder-only scenarios (no Add/Delete; just element shuffle)
// ------------------------------------------------------------------

// TestUpgrade_InitContainer_ReorderOnly — source reorders the existing
// initContainers without adding or removing any. ComputeMutations records
// no path mutations but populates ArrayOrders so the reorder pass shuffles
// the target array.
func TestUpgrade_InitContainer_ReorderOnly(t *testing.T) {
	// Source swaps db-init and db-migrate without changing anything else.
	source := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  namespace: confighubplaceholder
  annotations:
    confighub.com/ResourceMergeID: 11111111-1111-1111-1111-111111111111
spec:
  replicas: 1
  selector:
    matchLabels:
      app: myapp
  template:
    spec:
      initContainers:
      - name: db-migrate
        image: ghcr.io/example/app:v1
        env:
        - name: ENV_A
          value: alpha
        - name: ENV_B
          value: bravo
      - name: db-init
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
	t.Run("no override applies reorder", func(t *testing.T) {
		out := runUpgrade(t, baseDeploymentTwoInits, source, targetSameAsBase)
		assert.Equal(t, []string{"db-migrate", "db-init"}, initContainerNames(t, out))
	})

	t.Run("with target image override on a reordered container", func(t *testing.T) {
		// Target customized db-init's image. The reorder pass still applies
		// (source's intent is to swap their order). The override survives
		// because it's on a path the source didn't touch.
		target := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  namespace: confighubplaceholder
  annotations:
    confighub.com/ResourceMergeID: 11111111-1111-1111-1111-111111111111
spec:
  replicas: 1
  selector:
    matchLabels:
      app: myapp
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
        - name: ENV_B
          value: bravo
      containers:
      - name: app
        image: ghcr.io/example/app:v1
`
		out := runUpgrade(t, baseDeploymentTwoInits, source, target)
		assert.Equal(t, []string{"db-migrate", "db-init"}, initContainerNames(t, out))
		// db-init's customized image survives.
		docs, err := gaby.ParseAll(out)
		require.NoError(t, err)
		var dbInitImage string
		for _, c := range docs[0].S("spec", "template", "spec", "initContainers").Children() {
			if c.S("name").Data().(string) == "db-init" {
				dbInitImage = c.S("image").Data().(string)
			}
		}
		assert.Equal(t, "ghcr.io/example/app:v3", dbInitImage)
	})
}

// ------------------------------------------------------------------
// Target-side insertion preservation (LCS-merged ArrayOrders)
// ------------------------------------------------------------------
//
// These tests cover the case where the target (downstream) has its own
// container that the source (upstream) doesn't know about. When source
// adds, removes, or reorders init containers, the target-side custom
// container should stay anchored to its target-side neighbors rather
// than being shoved to the end.

// targetWithCustomMiddleInit returns a target manifest with a downstream-
// only init container "custom-init" inserted between db-init and db-migrate.
const targetWithCustomMiddleInit = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  namespace: confighubplaceholder
  annotations:
    confighub.com/ResourceMergeID: 11111111-1111-1111-1111-111111111111
spec:
  replicas: 1
  selector:
    matchLabels:
      app: myapp
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
      - name: custom-init
        image: ghcr.io/example/custom:v1
      - name: db-migrate
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

// TestUpgrade_InitContainer_TargetInsertPreservedAcrossSourceAdd — source
// adds db-pre at the front. The target's "custom-init" between db-init and
// db-migrate stays in that relative position thanks to the LCS-merged
// ArrayOrders (target's preceding common is db-init, source places
// db-pre at the front, db-init keeps its source-side position, custom-init
// follows db-init per target's order).
func TestUpgrade_InitContainer_TargetInsertPreservedAcrossSourceAdd(t *testing.T) {
	source := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  namespace: confighubplaceholder
  annotations:
    confighub.com/ResourceMergeID: 11111111-1111-1111-1111-111111111111
spec:
  replicas: 1
  selector:
    matchLabels:
      app: myapp
  template:
    spec:
      initContainers:
      - name: db-pre
        image: ghcr.io/example/app:v1
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
        - name: ENV_B
          value: bravo
      containers:
      - name: app
        image: ghcr.io/example/app:v1
`
	out := runUpgrade(t, baseDeploymentTwoInits, source, targetWithCustomMiddleInit)
	assert.Equal(t,
		[]string{"db-pre", "db-init", "custom-init", "db-migrate"},
		initContainerNames(t, out))
}

// TestUpgrade_InitContainer_TargetInsertPreservedAcrossSourceReorder — source
// swaps db-init and db-migrate. Target's "custom-init" was anchored to
// db-init (its preceding common in target). After the merge, custom-init
// follows db-init in the result, even though db-init has moved.
func TestUpgrade_InitContainer_TargetInsertPreservedAcrossSourceReorder(t *testing.T) {
	source := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  namespace: confighubplaceholder
  annotations:
    confighub.com/ResourceMergeID: 11111111-1111-1111-1111-111111111111
spec:
  replicas: 1
  selector:
    matchLabels:
      app: myapp
  template:
    spec:
      initContainers:
      - name: db-migrate
        image: ghcr.io/example/app:v1
        env:
        - name: ENV_A
          value: alpha
        - name: ENV_B
          value: bravo
      - name: db-init
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
	out := runUpgrade(t, baseDeploymentTwoInits, source, targetWithCustomMiddleInit)
	// db-init moved to position 1 (after db-migrate); custom-init follows
	// db-init per target's "after db-init" anchor.
	assert.Equal(t,
		[]string{"db-migrate", "db-init", "custom-init"},
		initContainerNames(t, out))
}

// TestUpgrade_InitContainer_TargetInsertWithSourceAddBetween — source adds an
// element between db-init and db-migrate; target also has its own element
// between db-init and db-migrate. Both sides anchor the new element to
// db-init. The merge convention places source-only first, then target-only.
func TestUpgrade_InitContainer_TargetInsertWithSourceAddBetween(t *testing.T) {
	source := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  namespace: confighubplaceholder
  annotations:
    confighub.com/ResourceMergeID: 11111111-1111-1111-1111-111111111111
spec:
  replicas: 1
  selector:
    matchLabels:
      app: myapp
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
      - name: db-mid
        image: ghcr.io/example/app:v1
      - name: db-migrate
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
	out := runUpgrade(t, baseDeploymentTwoInits, source, targetWithCustomMiddleInit)
	// Source's db-mid emits right after db-init (source's spine);
	// target's custom-init emits next (target-only attached to db-init);
	// then db-migrate.
	assert.Equal(t,
		[]string{"db-init", "db-mid", "custom-init", "db-migrate"},
		initContainerNames(t, out))
}

// ------------------------------------------------------------------
// env-var scenarios (merge key: name on env, nested under initContainers)
// ------------------------------------------------------------------

func TestUpgrade_InitContainerEnv_Change(t *testing.T) {
	// Source: change ENV_A's value on db-init.
	source := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  namespace: confighubplaceholder
  annotations:
    confighub.com/ResourceMergeID: 11111111-1111-1111-1111-111111111111
spec:
  replicas: 1
  selector:
    matchLabels:
      app: myapp
  template:
    spec:
      initContainers:
      - name: db-init
        image: ghcr.io/example/app:v1
        env:
        - name: ENV_A
          value: ALPHA-2
        - name: ENV_B
          value: bravo
      - name: db-migrate
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

	t.Run("no override propagates change", func(t *testing.T) {
		out := runUpgrade(t, baseDeploymentTwoInits, source, targetSameAsBase)
		assert.Equal(t, "ALPHA-2", initContainerEnvValue(t, out, "db-init", "ENV_A"))
		assert.Equal(t, "alpha", initContainerEnvValue(t, out, "db-migrate", "ENV_A"))
	})

	t.Run("target ENV_A override is preserved", func(t *testing.T) {
		target := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  namespace: confighubplaceholder
  annotations:
    confighub.com/ResourceMergeID: 11111111-1111-1111-1111-111111111111
spec:
  replicas: 1
  selector:
    matchLabels:
      app: myapp
  template:
    spec:
      initContainers:
      - name: db-init
        image: ghcr.io/example/app:v1
        env:
        - name: ENV_A
          value: ALPHA-TARGET
        - name: ENV_B
          value: bravo
      - name: db-migrate
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
		out := runUpgrade(t, baseDeploymentTwoInits, source, target)
		assert.Equal(t, "ALPHA-TARGET", initContainerEnvValue(t, out, "db-init", "ENV_A"))
	})
}

func TestUpgrade_InitContainerEnv_Addition(t *testing.T) {
	// Source: append ENV_C on db-init.
	source := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  namespace: confighubplaceholder
  annotations:
    confighub.com/ResourceMergeID: 11111111-1111-1111-1111-111111111111
spec:
  replicas: 1
  selector:
    matchLabels:
      app: myapp
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
        - name: ENV_C
          value: charlie
      - name: db-migrate
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

	t.Run("no override appends env var", func(t *testing.T) {
		out := runUpgrade(t, baseDeploymentTwoInits, source, targetSameAsBase)
		assert.Equal(t, []string{"ENV_A", "ENV_B", "ENV_C"}, initContainerEnvNames(t, out, "db-init"))
		assert.Equal(t, "charlie", initContainerEnvValue(t, out, "db-init", "ENV_C"))
	})

	t.Run("with target override on a different env var", func(t *testing.T) {
		target := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  namespace: confighubplaceholder
  annotations:
    confighub.com/ResourceMergeID: 11111111-1111-1111-1111-111111111111
spec:
  replicas: 1
  selector:
    matchLabels:
      app: myapp
  template:
    spec:
      initContainers:
      - name: db-init
        image: ghcr.io/example/app:v1
        env:
        - name: ENV_A
          value: ALPHA-TARGET
        - name: ENV_B
          value: bravo
      - name: db-migrate
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
		out := runUpgrade(t, baseDeploymentTwoInits, source, target)
		assert.Equal(t, []string{"ENV_A", "ENV_B", "ENV_C"}, initContainerEnvNames(t, out, "db-init"))
		assert.Equal(t, "ALPHA-TARGET", initContainerEnvValue(t, out, "db-init", "ENV_A"))
		assert.Equal(t, "charlie", initContainerEnvValue(t, out, "db-init", "ENV_C"))
	})
}

func TestUpgrade_InitContainerEnv_Removal(t *testing.T) {
	// Source: remove ENV_B on db-init.
	source := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  namespace: confighubplaceholder
  annotations:
    confighub.com/ResourceMergeID: 11111111-1111-1111-1111-111111111111
spec:
  replicas: 1
  selector:
    matchLabels:
      app: myapp
  template:
    spec:
      initContainers:
      - name: db-init
        image: ghcr.io/example/app:v1
        env:
        - name: ENV_A
          value: alpha
      - name: db-migrate
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

	t.Run("no override removes env var", func(t *testing.T) {
		out := runUpgrade(t, baseDeploymentTwoInits, source, targetSameAsBase)
		assert.Equal(t, []string{"ENV_A"}, initContainerEnvNames(t, out, "db-init"))
	})

	t.Run("target override on removed env var is shadowed", func(t *testing.T) {
		// Target changed ENV_B's value; the source-side Delete of ENV_B
		// passes through (the parent element being removed wins). ENV_B
		// disappears from db-init's env. The .value override is reported
		// as a DeleteShadowed conflict by SubtractMutations; runUpgrade
		// discards it here.
		target := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  namespace: confighubplaceholder
  annotations:
    confighub.com/ResourceMergeID: 11111111-1111-1111-1111-111111111111
spec:
  replicas: 1
  selector:
    matchLabels:
      app: myapp
  template:
    spec:
      initContainers:
      - name: db-init
        image: ghcr.io/example/app:v1
        env:
        - name: ENV_A
          value: alpha
        - name: ENV_B
          value: BRAVO-TARGET
      - name: db-migrate
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
		out := runUpgrade(t, baseDeploymentTwoInits, source, target)
		assert.Equal(t, []string{"ENV_A"}, initContainerEnvNames(t, out, "db-init"))
	})
}

func TestUpgrade_InitContainerEnv_Rename(t *testing.T) {
	// Source: rename ENV_B -> ENV_BETA on db-init. Same Delete+Add pattern as
	// the initContainer rename, but at the deeper merge-key level.
	source := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  namespace: confighubplaceholder
  annotations:
    confighub.com/ResourceMergeID: 11111111-1111-1111-1111-111111111111
spec:
  replicas: 1
  selector:
    matchLabels:
      app: myapp
  template:
    spec:
      initContainers:
      - name: db-init
        image: ghcr.io/example/app:v1
        env:
        - name: ENV_A
          value: alpha
        - name: ENV_BETA
          value: bravo
      - name: db-migrate
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

	t.Run("no override applies rename", func(t *testing.T) {
		out := runUpgrade(t, baseDeploymentTwoInits, source, targetSameAsBase)
		assert.Equal(t, []string{"ENV_A", "ENV_BETA"}, initContainerEnvNames(t, out, "db-init"))
	})

	t.Run("override on renamed env var is shadowed; rename completes", func(t *testing.T) {
		target := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  namespace: confighubplaceholder
  annotations:
    confighub.com/ResourceMergeID: 11111111-1111-1111-1111-111111111111
spec:
  replicas: 1
  selector:
    matchLabels:
      app: myapp
  template:
    spec:
      initContainers:
      - name: db-init
        image: ghcr.io/example/app:v1
        env:
        - name: ENV_A
          value: alpha
        - name: ENV_B
          value: BRAVO-TARGET
      - name: db-migrate
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
		out := runUpgrade(t, baseDeploymentTwoInits, source, target)
		// ENV_B's source-side Delete passes through (parent-removal wins
		// over child override); ENV_BETA is appended. The .value override is
		// reported as a DeleteShadowed conflict by SubtractMutations.
		assert.Equal(t, []string{"ENV_A", "ENV_BETA"}, initContainerEnvNames(t, out, "db-init"))
	})
}

func TestUpgrade_InitContainerEnv_Insertion(t *testing.T) {
	// Source: insert ENV_PRE at index 0 on db-init.
	source := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  namespace: confighubplaceholder
  annotations:
    confighub.com/ResourceMergeID: 11111111-1111-1111-1111-111111111111
spec:
  replicas: 1
  selector:
    matchLabels:
      app: myapp
  template:
    spec:
      initContainers:
      - name: db-init
        image: ghcr.io/example/app:v1
        env:
        - name: ENV_PRE
          value: pre
        - name: ENV_A
          value: alpha
        - name: ENV_B
          value: bravo
      - name: db-migrate
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

	t.Run("no override applies insertion", func(t *testing.T) {
		out := runUpgrade(t, baseDeploymentTwoInits, source, targetSameAsBase)
		got := initContainerEnvNames(t, out, "db-init")
		assert.Contains(t, got, "ENV_PRE")
		assert.Contains(t, got, "ENV_A")
		assert.Contains(t, got, "ENV_B")
	})

	t.Run("with target override on a shifted env var", func(t *testing.T) {
		target := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  namespace: confighubplaceholder
  annotations:
    confighub.com/ResourceMergeID: 11111111-1111-1111-1111-111111111111
spec:
  replicas: 1
  selector:
    matchLabels:
      app: myapp
  template:
    spec:
      initContainers:
      - name: db-init
        image: ghcr.io/example/app:v1
        env:
        - name: ENV_A
          value: ALPHA-TARGET
        - name: ENV_B
          value: bravo
      - name: db-migrate
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
		out := runUpgrade(t, baseDeploymentTwoInits, source, target)
		got := initContainerEnvNames(t, out, "db-init")
		assert.Contains(t, got, "ENV_PRE")
		assert.Contains(t, got, "ENV_A")
		assert.Contains(t, got, "ENV_B")
		assert.Equal(t, "ALPHA-TARGET", initContainerEnvValue(t, out, "db-init", "ENV_A"))
	})
}

// ------------------------------------------------------------------
// Cross-scenario sanity: subtraction is opt-in via mutationsToSubtract
// ------------------------------------------------------------------

// TestUpgrade_PatchMutations_NoSubtractArgument verifies that passing nil for
// mutationsToSubtract makes PatchMutations behave identically to applying the
// raw source mutations (no three-way merge).
func TestUpgrade_PatchMutations_NoSubtractArgument(t *testing.T) {
	provider := k8skit.NewK8sResourceProvider()

	// Source removes db-init.
	source := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  namespace: confighubplaceholder
  annotations:
    confighub.com/ResourceMergeID: 11111111-1111-1111-1111-111111111111
spec:
  replicas: 1
  selector:
    matchLabels:
      app: myapp
  template:
    spec:
      initContainers:
      - name: db-migrate
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
	// Target customized db-init.image — would normally protect db-init.
	target := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  namespace: confighubplaceholder
  annotations:
    confighub.com/ResourceMergeID: 11111111-1111-1111-1111-111111111111
spec:
  replicas: 1
  selector:
    matchLabels:
      app: myapp
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
        - name: ENV_B
          value: bravo
      containers:
      - name: app
        image: ghcr.io/example/app:v1
`

	baseParsed, err := gaby.ParseAll([]byte(baseDeploymentTwoInits))
	require.NoError(t, err)
	sourceParsed, err := gaby.ParseAll([]byte(source))
	require.NoError(t, err)
	targetParsed, err := gaby.ParseAll([]byte(target))
	require.NoError(t, err)

	sourceMutations, err := yamlkit.ComputeMutations(baseParsed, sourceParsed, 1, provider)
	require.NoError(t, err)

	// Pass nil for mutationsToSubtract — no protection of target overrides.
	patched, _, err := yamlkit.PatchMutations(targetParsed, nil, sourceMutations, nil, provider, nil)
	require.NoError(t, err)

	// db-init was removed even though target customized its image, because
	// no subtract list was supplied.
	assert.Equal(t, []string{"db-migrate"}, initContainerNames(t, patched.Bytes()))
}

// TestUpgrade_PatchMutations_EmptySubtractEquivalentToNil confirms that an
// empty (non-nil but len 0) subtract list is treated the same as nil.
func TestUpgrade_PatchMutations_EmptySubtractEquivalentToNil(t *testing.T) {
	provider := k8skit.NewK8sResourceProvider()
	baseParsed, err := gaby.ParseAll([]byte(baseDeploymentTwoInits))
	require.NoError(t, err)

	// Source: change db-init image.
	source := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  namespace: confighubplaceholder
  annotations:
    confighub.com/ResourceMergeID: 11111111-1111-1111-1111-111111111111
spec:
  replicas: 1
  selector:
    matchLabels:
      app: myapp
  template:
    spec:
      initContainers:
      - name: db-init
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
        - name: ENV_B
          value: bravo
      containers:
      - name: app
        image: ghcr.io/example/app:v1
`
	sourceParsed, err := gaby.ParseAll([]byte(source))
	require.NoError(t, err)
	targetParsed, err := gaby.ParseAll([]byte(targetSameAsBase))
	require.NoError(t, err)

	sourceMutations, err := yamlkit.ComputeMutations(baseParsed, sourceParsed, 1, provider)
	require.NoError(t, err)

	emptyList := api.ResourceMutationList{}
	patchedEmpty, _, err := yamlkit.PatchMutations(targetParsed, nil, sourceMutations, emptyList, provider, nil)
	require.NoError(t, err)

	// PatchMutations mutates parsedData in place, so reparse the target for
	// the second call.
	targetReparsed, err := gaby.ParseAll([]byte(targetSameAsBase))
	require.NoError(t, err)
	patchedNil, _, err := yamlkit.PatchMutations(targetReparsed, nil, sourceMutations, nil, provider, nil)
	require.NoError(t, err)

	assert.Equal(t, string(patchedNil.Bytes()), string(patchedEmpty.Bytes()))
}
