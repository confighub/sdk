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

// Tests for the MutationConflict reporting added to SubtractMutations and
// PatchMutations. The patched output must be unchanged from before; the new
// observability is an additional return value listing every dropped mutation.

// reasonsByPath collects conflict reasons keyed by path (or "<resource>" when
// the conflict is resource-level).
func reasonsByPath(conflicts api.MutationConflictList) map[string]api.ConflictReason {
	out := map[string]api.ConflictReason{}
	for _, c := range conflicts {
		if c.Path == "" {
			out["<resource>"] = c.Reason
		} else {
			out[string(c.Path)] = c.Reason
		}
	}
	return out
}

// findConflict returns the first conflict matching path (use "" for resource-level)
// and the resource ResourceNameWithoutScope.
func findConflict(t *testing.T, conflicts api.MutationConflictList, resourceName, path string) api.MutationConflict {
	t.Helper()
	for _, c := range conflicts {
		if string(c.Resource.ResourceNameWithoutScope) != resourceName {
			continue
		}
		if string(c.Path) == path {
			return c
		}
	}
	t.Fatalf("no conflict found for resource %q path %q in %d conflicts", resourceName, path, len(conflicts))
	return api.MutationConflict{}
}

// ---------------------------------------------------------------------------
// SubtractMutations conflict emission
// ---------------------------------------------------------------------------

func TestSubtractMutations_PathLevel_ExactMatchEmitsSubtracted(t *testing.T) {
	provider := k8skit.NewK8sResourceProvider()
	base := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: app
  namespace: default
spec:
  replicas: 1
`
	source := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: app
  namespace: default
spec:
  replicas: 5
`
	target := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: app
  namespace: default
spec:
  replicas: 7
`
	baseP, _ := gaby.ParseAll([]byte(base))
	srcP, _ := gaby.ParseAll([]byte(source))
	tgtP, _ := gaby.ParseAll([]byte(target))

	srcMut, err := yamlkit.ComputeMutations(baseP, srcP, 1, provider)
	require.NoError(t, err)
	tgtMut, err := yamlkit.ComputeMutations(baseP, tgtP, 2, provider)
	require.NoError(t, err)

	_, conflicts := yamlkit.SubtractMutations(srcMut, tgtMut)
	require.Len(t, conflicts, 1)
	c := conflicts[0]
	assert.Equal(t, api.ConflictReasonSubtracted, c.Reason)
	assert.Equal(t, api.ResolvedPath("spec.replicas"), c.Path)
	assert.Equal(t, "app", string(c.Resource.ResourceNameWithoutScope))
	require.NotNil(t, c.Target)
	assert.Equal(t, "7\n", c.Target.Value)
	assert.Equal(t, "5\n", c.Source.Value)
}

func TestSubtractMutations_PathLevel_AncestorEmitsSubtracted(t *testing.T) {
	provider := k8skit.NewK8sResourceProvider()
	base := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: app
  namespace: default
spec:
  template:
    spec:
      containers:
      - name: app
        image: nginx:1.19
        resources:
          requests:
            cpu: 500m
            memory: 256Mi
`
	// Source modifies only requests.cpu.
	source := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: app
  namespace: default
spec:
  template:
    spec:
      containers:
      - name: app
        image: nginx:1.19
        resources:
          requests:
            cpu: 750m
            memory: 256Mi
`
	// Target replaces the whole resources.requests block (different shape than source).
	target := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: app
  namespace: default
spec:
  template:
    spec:
      containers:
      - name: app
        image: nginx:1.19
        resources:
          requests:
            cpu: 100m
            memory: 64Mi
            ephemeral-storage: 1Gi
`
	baseP, _ := gaby.ParseAll([]byte(base))
	srcP, _ := gaby.ParseAll([]byte(source))
	tgtP, _ := gaby.ParseAll([]byte(target))

	srcMut, err := yamlkit.ComputeMutations(baseP, srcP, 1, provider)
	require.NoError(t, err)
	tgtMut, err := yamlkit.ComputeMutations(baseP, tgtP, 2, provider)
	require.NoError(t, err)

	_, conflicts := yamlkit.SubtractMutations(srcMut, tgtMut)
	// Source's requests.cpu should be subtracted because target also touched it.
	require.NotEmpty(t, conflicts)
	got := reasonsByPath(conflicts)
	for path, reason := range got {
		assert.Equal(t, api.ConflictReasonSubtracted, reason, "path %s", path)
	}
}

func TestSubtractMutations_DeleteShadowed_AtPathLevel(t *testing.T) {
	provider := k8skit.NewK8sResourceProvider()
	base := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: app
  namespace: default
spec:
  template:
    spec:
      containers:
      - name: app
        image: nginx:1.19
        resources:
          requests:
            cpu: 500m
            memory: 256Mi
`
	// Source removes the resources block entirely.
	source := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: app
  namespace: default
spec:
  template:
    spec:
      containers:
      - name: app
        image: nginx:1.19
`
	// Target only changed cpu within the resources block.
	target := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: app
  namespace: default
spec:
  template:
    spec:
      containers:
      - name: app
        image: nginx:1.19
        resources:
          requests:
            cpu: 750m
            memory: 256Mi
`
	baseP, _ := gaby.ParseAll([]byte(base))
	srcP, _ := gaby.ParseAll([]byte(source))
	tgtP, _ := gaby.ParseAll([]byte(target))

	srcMut, err := yamlkit.ComputeMutations(baseP, srcP, 1, provider)
	require.NoError(t, err)
	tgtMut, err := yamlkit.ComputeMutations(baseP, tgtP, 2, provider)
	require.NoError(t, err)

	result, conflicts := yamlkit.SubtractMutations(srcMut, tgtMut)
	// The source-side resources Delete passes through.
	resourcesPath := api.ResolvedPath("spec.template.spec.containers.?name=app;@0.resources")
	require.Len(t, result, 1)
	resMut, hasDelete := result[0].PathMutationMap[resourcesPath]
	require.True(t, hasDelete, "source-side resources Delete should pass through")
	assert.Equal(t, api.MutationTypeDelete, resMut.MutationType)

	// The target's child mutations under that path get DeleteShadowed conflicts —
	// one per shadowed child path. The conflict's Path is the target's child
	// path, the Source is the source's parent-level Delete.
	cpuPath := api.ResolvedPath("spec.template.spec.containers.?name=app;@0.resources.requests.cpu")
	var deleteShadowed *api.MutationConflict
	for i := range conflicts {
		if conflicts[i].Reason == api.ConflictReasonDeleteShadowed && conflicts[i].Path == cpuPath {
			deleteShadowed = &conflicts[i]
			break
		}
	}
	require.NotNil(t, deleteShadowed, "expected a DeleteShadowed conflict for the lost cpu override")
	assert.Equal(t, api.MutationTypeDelete, deleteShadowed.Source.MutationType,
		"Source is the source-side parent Delete")
	require.NotNil(t, deleteShadowed.Target, "Target is the target-side child mutation")
	assert.Equal(t, api.MutationTypeUpdate, deleteShadowed.Target.MutationType)
}

// TestSubtractMutations_DeleteShadowed_LetsDeleteThrough — the source-side
// Delete now passes through to the result rather than being dropped, matching
// the loosened SubtractMutations semantics. Previously the test asserting the
// "Delete dropped" behavior is now in TestConflict_SourceRemovesResources_*
// (in yamlkit_baseline_test.go) which verifies the resources block is gone
// from the patched output.
func TestSubtractMutations_DeleteShadowed_LetsDeleteThrough(t *testing.T) {
	provider := k8skit.NewK8sResourceProvider()
	base := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: app
  namespace: default
spec:
  template:
    spec:
      containers:
      - name: app
        image: nginx:1.19
        resources:
          requests:
            cpu: 500m
            memory: 256Mi
`
	source := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: app
  namespace: default
spec:
  template:
    spec:
      containers:
      - name: app
        image: nginx:1.19
`
	target := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: app
  namespace: default
spec:
  template:
    spec:
      containers:
      - name: app
        image: nginx:1.19
        resources:
          requests:
            cpu: 750m
            memory: 256Mi
`
	baseP, _ := gaby.ParseAll([]byte(base))
	srcP, _ := gaby.ParseAll([]byte(source))
	tgtP, _ := gaby.ParseAll([]byte(target))
	srcMut, _ := yamlkit.ComputeMutations(baseP, srcP, 1, provider)
	tgtMut, _ := yamlkit.ComputeMutations(baseP, tgtP, 2, provider)

	result, conflicts := yamlkit.SubtractMutations(srcMut, tgtMut)
	resourcesPath := api.ResolvedPath("spec.template.spec.containers.?name=app;@0.resources")
	require.Len(t, result, 1)
	_, hasDelete := result[0].PathMutationMap[resourcesPath]
	assert.True(t, hasDelete, "source-side parent Delete passes through")

	// Each shadowed child gets its own DeleteShadowed conflict.
	var shadowedPaths []api.ResolvedPath
	for _, c := range conflicts {
		if c.Reason == api.ConflictReasonDeleteShadowed {
			shadowedPaths = append(shadowedPaths, c.Path)
		}
	}
	assert.Contains(t, shadowedPaths,
		api.ResolvedPath("spec.template.spec.containers.?name=app;@0.resources.requests.cpu"))
}

func TestSubtractMutations_ResourceLevel_SourceDeleteVsTargetUpdateLetsDeleteThrough(t *testing.T) {
	provider := k8skit.NewK8sResourceProvider()
	base := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: app
  namespace: default
spec:
  replicas: 1
`
	// Target deletes the resource (modified is empty), so the source-Delete
	// vs target-Update case requires source to delete and target to update.
	// Flip: source deletes, target keeps but modifies.
	source := ``
	target := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: app
  namespace: default
spec:
  replicas: 5
`
	baseP, _ := gaby.ParseAll([]byte(base))
	srcP, _ := gaby.ParseAll([]byte(source))
	tgtP, _ := gaby.ParseAll([]byte(target))
	srcMut, _ := yamlkit.ComputeMutations(baseP, srcP, 1, provider)
	tgtMut, _ := yamlkit.ComputeMutations(baseP, tgtP, 2, provider)

	result, conflicts := yamlkit.SubtractMutations(srcMut, tgtMut)
	require.Len(t, result, 1)
	assert.Equal(t, api.MutationTypeDelete, result[0].ResourceMutationInfo.MutationType,
		"source-side resource-level Delete passes through")

	// Resource-level + per-path DeleteShadowed conflicts emitted for the
	// target's edits.
	var resourceLevel, pathLevel int
	for _, c := range conflicts {
		if c.Reason == api.ConflictReasonDeleteShadowed {
			if c.Path == "" {
				resourceLevel++
			} else {
				pathLevel++
			}
		}
	}
	assert.GreaterOrEqual(t, resourceLevel, 1, "resource-level DeleteShadowed conflict")
	assert.GreaterOrEqual(t, pathLevel, 1, "at least one path-level DeleteShadowed conflict")
}

func TestSubtractMutations_ResourceLevel_DeleteEmitsSubtracted(t *testing.T) {
	provider := k8skit.NewK8sResourceProvider()
	base := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: app
  namespace: default
spec:
  replicas: 1
`
	// Source modifies the deployment.
	source := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: app
  namespace: default
spec:
  replicas: 5
`
	// Target deleted the deployment (modified is empty).
	baseP, _ := gaby.ParseAll([]byte(base))
	srcP, _ := gaby.ParseAll([]byte(source))
	tgtP, _ := gaby.ParseAll([]byte(""))

	srcMut, err := yamlkit.ComputeMutations(baseP, srcP, 1, provider)
	require.NoError(t, err)
	tgtMut, err := yamlkit.ComputeMutations(baseP, tgtP, 2, provider)
	require.NoError(t, err)

	_, conflicts := yamlkit.SubtractMutations(srcMut, tgtMut)
	require.Len(t, conflicts, 1)
	c := conflicts[0]
	assert.Equal(t, api.ConflictReasonSubtracted, c.Reason)
	assert.Equal(t, api.ResolvedPath(""), c.Path, "resource-level conflict has empty path")
	require.NotNil(t, c.Target)
	assert.Equal(t, api.MutationTypeDelete, c.Target.MutationType)
}

func TestSubtractMutations_ResourceLevel_DeleteVsDeleteEmitsSubtracted(t *testing.T) {
	provider := k8skit.NewK8sResourceProvider()
	base := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: app
  namespace: default
spec:
  replicas: 1
`
	baseP, _ := gaby.ParseAll([]byte(base))
	emptyP, _ := gaby.ParseAll([]byte(""))

	// Both source and target deleted the resource.
	srcMut, err := yamlkit.ComputeMutations(baseP, emptyP, 1, provider)
	require.NoError(t, err)
	tgtMut, err := yamlkit.ComputeMutations(baseP, emptyP, 2, provider)
	require.NoError(t, err)

	_, conflicts := yamlkit.SubtractMutations(srcMut, tgtMut)
	require.Len(t, conflicts, 1)
	assert.Equal(t, api.ConflictReasonSubtracted, conflicts[0].Reason)
}

// ---------------------------------------------------------------------------
// PatchMutations conflict emission
// ---------------------------------------------------------------------------

const conflictTestBase = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: app
  namespace: default
spec:
  replicas: 1
  template:
    spec:
      containers:
      - name: app
        image: nginx:1.19
`

func deploymentPatchUpdate(replicas, image string) api.ResourceMutationList {
	return api.ResourceMutationList{{
		Resource: api.ResourceInfo{
			ResourceType:             "apps/v1/Deployment",
			ResourceName:             "default/app",
			ResourceNameWithoutScope: "app",
			ResourceCategory:         "Kubernetes",
		},
		ResourceMutationInfo: api.MutationInfo{
			MutationType: api.MutationTypeUpdate,
			Index:        2,
			Predicate:    true,
		},
		PathMutationMap: api.MutationMap{
			"spec.replicas": api.MutationInfo{
				MutationType: api.MutationTypeUpdate,
				Index:        2,
				Predicate:    true,
				Value:        replicas,
			},
			"spec.template.spec.containers.?name=app;@0.image": api.MutationInfo{
				MutationType: api.MutationTypeUpdate,
				Index:        2,
				Predicate:    true,
				Value:        image,
			},
		},
	}}
}

// TestPatch_SubtractConflictsAreForwarded — a path that gets subtracted produces
// a conflict in PatchMutations' return value.
func TestPatch_SubtractConflictsAreForwarded(t *testing.T) {
	provider := k8skit.NewK8sResourceProvider()
	parsed, _ := gaby.ParseAll([]byte(conflictTestBase))

	subtract := api.ResourceMutationList{{
		Resource: api.ResourceInfo{
			ResourceType:             "apps/v1/Deployment",
			ResourceName:             "default/app",
			ResourceNameWithoutScope: "app",
			ResourceCategory:         "Kubernetes",
		},
		ResourceMutationInfo: api.MutationInfo{
			MutationType: api.MutationTypeUpdate,
			Index:        3,
			Predicate:    true,
		},
		PathMutationMap: api.MutationMap{
			// Target also touched spec.replicas — should subtract source's update.
			"spec.replicas": api.MutationInfo{
				MutationType: api.MutationTypeUpdate,
				Index:        3,
				Predicate:    true,
				Value:        "9\n",
			},
		},
	}}
	_, conflicts, err := yamlkit.PatchMutations(parsed, nil, deploymentPatchUpdate("5\n", "nginx:1.20\n"), subtract, provider, nil)
	require.NoError(t, err)
	got := reasonsByPath(conflicts)
	assert.Equal(t, api.ConflictReasonSubtracted, got["spec.replicas"])
	assert.NotContains(t, got, "spec.template.spec.containers.?name=app;@0.image",
		"image path was unaffected by subtraction")
}

// TestPatch_PredicateFilteredAtPath emits a conflict for the filtered path.
func TestPatch_PredicateFilteredAtPath(t *testing.T) {
	provider := k8skit.NewK8sResourceProvider()
	parsed, _ := gaby.ParseAll([]byte(conflictTestBase))
	predicates := api.ResourceMutationList{{
		Resource: api.ResourceInfo{
			ResourceType:             "apps/v1/Deployment",
			ResourceName:             "default/app",
			ResourceNameWithoutScope: "app",
			ResourceCategory:         "Kubernetes",
		},
		ResourceMutationInfo: api.MutationInfo{MutationType: api.MutationTypeUpdate, Predicate: true},
		PathMutationMap: api.MutationMap{
			"spec.replicas": api.MutationInfo{Predicate: false}, // block replicas
		},
	}}
	_, conflicts, err := yamlkit.PatchMutations(parsed, predicates, deploymentPatchUpdate("5\n", "nginx:1.20\n"), nil, provider, nil)
	require.NoError(t, err)
	c := findConflict(t, conflicts, "app", "spec.replicas")
	assert.Equal(t, api.ConflictReasonPredicateFiltered, c.Reason)
	require.NotNil(t, c.Target)
	assert.False(t, c.Target.Predicate)
}

// TestPatch_PredicateFilteredAtAncestorEmitsConflictForChild — when a parent
// path has Predicate=false, child path mutations are dropped and conflicts
// are emitted for each child.
func TestPatch_PredicateFilteredAtAncestorEmitsConflictForChild(t *testing.T) {
	provider := k8skit.NewK8sResourceProvider()
	parsed, _ := gaby.ParseAll([]byte(conflictTestBase))
	predicates := api.ResourceMutationList{{
		Resource: api.ResourceInfo{
			ResourceType:             "apps/v1/Deployment",
			ResourceName:             "default/app",
			ResourceNameWithoutScope: "app",
			ResourceCategory:         "Kubernetes",
		},
		ResourceMutationInfo: api.MutationInfo{MutationType: api.MutationTypeUpdate, Predicate: true},
		PathMutationMap: api.MutationMap{
			// Block everything under spec.template.
			"spec.template": api.MutationInfo{Predicate: false},
		},
	}}
	_, conflicts, err := yamlkit.PatchMutations(parsed, predicates, deploymentPatchUpdate("5\n", "nginx:1.20\n"), nil, provider, nil)
	require.NoError(t, err)
	got := reasonsByPath(conflicts)
	assert.Equal(t, api.ConflictReasonPredicateFiltered, got["spec.template.spec.containers.?name=app;@0.image"])
	assert.NotContains(t, got, "spec.replicas",
		"replicas isn't under spec.template, should not be filtered")
}

// TestPatch_PredicateFilteredAtResourceLevel emits a single resource-level
// PredicateFiltered conflict.
func TestPatch_PredicateFilteredAtResourceLevel(t *testing.T) {
	provider := k8skit.NewK8sResourceProvider()
	parsed, _ := gaby.ParseAll([]byte(conflictTestBase))
	predicates := api.ResourceMutationList{{
		Resource: api.ResourceInfo{
			ResourceType:             "apps/v1/Deployment",
			ResourceName:             "default/app",
			ResourceNameWithoutScope: "app",
			ResourceCategory:         "Kubernetes",
		},
		ResourceMutationInfo: api.MutationInfo{
			MutationType: api.MutationTypeUpdate,
			Predicate:    false, // block whole resource
		},
	}}
	_, conflicts, err := yamlkit.PatchMutations(parsed, predicates, deploymentPatchUpdate("5\n", "nginx:1.20\n"), nil, provider, nil)
	require.NoError(t, err)
	require.Len(t, conflicts, 1)
	c := conflicts[0]
	assert.Equal(t, api.ConflictReasonPredicateFiltered, c.Reason)
	assert.Equal(t, api.ResolvedPath(""), c.Path, "resource-level conflict has empty path")
}

// TestPatch_UnresolvedPathDelete — a Delete whose merge-key value doesn't match
// any element in the target produces an UnresolvedPath conflict.
func TestPatch_UnresolvedPathDelete(t *testing.T) {
	provider := k8skit.NewK8sResourceProvider()
	target := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: app
  namespace: default
spec:
  template:
    spec:
      containers:
      - name: only-container
        image: nginx:1.19
`
	parsed, _ := gaby.ParseAll([]byte(target))

	patch := api.ResourceMutationList{{
		Resource: api.ResourceInfo{
			ResourceType:             "apps/v1/Deployment",
			ResourceName:             "default/app",
			ResourceNameWithoutScope: "app",
			ResourceCategory:         "Kubernetes",
		},
		ResourceMutationInfo: api.MutationInfo{MutationType: api.MutationTypeUpdate, Predicate: true},
		PathMutationMap: api.MutationMap{
			// Delete a container that doesn't exist in the target.
			"spec.template.spec.containers.?name=ghost;@0": api.MutationInfo{
				MutationType: api.MutationTypeDelete,
				Index:        2,
				Predicate:    true,
				Value:        "name: ghost\nimage: ghost:v1\n",
			},
		},
	}}
	_, conflicts, err := yamlkit.PatchMutations(parsed, nil, patch, nil, provider, nil)
	require.NoError(t, err)
	require.Len(t, conflicts, 1)
	c := conflicts[0]
	assert.Equal(t, api.ConflictReasonUnresolvedPath, c.Reason)
	assert.Equal(t, api.ResolvedPath("spec.template.spec.containers.?name=ghost;@0"), c.Path)
	assert.Equal(t, api.MutationTypeDelete, c.Source.MutationType)
}

// TestPatch_UnresolvedPathUpdate — an Update whose merge-key value doesn't
// match any element produces an UnresolvedPath conflict.
func TestPatch_UnresolvedPathUpdate(t *testing.T) {
	provider := k8skit.NewK8sResourceProvider()
	target := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: app
  namespace: default
spec:
  template:
    spec:
      containers:
      - name: only-container
        image: nginx:1.19
`
	parsed, _ := gaby.ParseAll([]byte(target))

	patch := api.ResourceMutationList{{
		Resource: api.ResourceInfo{
			ResourceType:             "apps/v1/Deployment",
			ResourceName:             "default/app",
			ResourceNameWithoutScope: "app",
			ResourceCategory:         "Kubernetes",
		},
		ResourceMutationInfo: api.MutationInfo{MutationType: api.MutationTypeUpdate, Predicate: true},
		PathMutationMap: api.MutationMap{
			"spec.template.spec.containers.?name=ghost;@0.image": api.MutationInfo{
				MutationType: api.MutationTypeUpdate,
				Index:        2,
				Predicate:    true,
				Value:        "redis:7\n",
			},
		},
	}}
	_, conflicts, err := yamlkit.PatchMutations(parsed, nil, patch, nil, provider, nil)
	require.NoError(t, err)
	require.Len(t, conflicts, 1)
	assert.Equal(t, api.ConflictReasonUnresolvedPath, conflicts[0].Reason)
}

// TestPatch_AddAppendDoesNotEmitConflict — when an Add's merge-key value isn't
// in the target and the index is out-of-bounds (or the parent array's existing
// elements have different keys), the Add is appended. This is "applied at a
// different index", not a drop, so no conflict is emitted.
func TestPatch_AddAppendDoesNotEmitConflict(t *testing.T) {
	provider := k8skit.NewK8sResourceProvider()
	target := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: app
  namespace: default
spec:
  template:
    spec:
      containers:
      - name: existing
        image: nginx:1.19
`
	parsed, _ := gaby.ParseAll([]byte(target))

	patch := api.ResourceMutationList{{
		Resource: api.ResourceInfo{
			ResourceType:             "apps/v1/Deployment",
			ResourceName:             "default/app",
			ResourceNameWithoutScope: "app",
			ResourceCategory:         "Kubernetes",
		},
		ResourceMutationInfo: api.MutationInfo{MutationType: api.MutationTypeUpdate, Predicate: true},
		PathMutationMap: api.MutationMap{
			"spec.template.spec.containers.?name=newone;@0": api.MutationInfo{
				MutationType: api.MutationTypeAdd,
				Index:        2,
				Predicate:    true,
				Value:        "name: newone\nimage: redis:7\n",
			},
		},
	}}
	_, conflicts, err := yamlkit.PatchMutations(parsed, nil, patch, nil, provider, nil)
	require.NoError(t, err)
	assert.Empty(t, conflicts, "Add appended at end is not a conflict")
}

// TestPatch_NoConflicts — happy path: no subtraction, all predicates true,
// every path resolved. Conflicts list is empty.
func TestPatch_NoConflicts(t *testing.T) {
	provider := k8skit.NewK8sResourceProvider()
	parsed, _ := gaby.ParseAll([]byte(conflictTestBase))
	_, conflicts, err := yamlkit.PatchMutations(parsed, nil, deploymentPatchUpdate("5\n", "nginx:1.20\n"), nil, provider, nil)
	require.NoError(t, err)
	assert.Empty(t, conflicts)
}
