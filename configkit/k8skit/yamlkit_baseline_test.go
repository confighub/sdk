// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package k8skit_test

import (
	"strings"
	"testing"

	"github.com/confighub/sdk/configkit/k8skit"
	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/third_party/gaby"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Tests in this file pin current behavior of ComputeMutations and PatchMutations
// for scenarios that lacked direct coverage. They serve as a regression baseline
// for upcoming PRs that change semantics around rename detection, positional
// associative arrays, conflict reporting, and SubtractMutations Delete handling.
//
// Coverage adds:
//
//   - Resource-level rename heuristic (the fuzzy-similarity branch in
//     ComputeMutations) — name change with multiple candidates, namespace move,
//     ResourceMergeID precedence, type change, no-rename baseline.
//   - Predicate filtering in PatchMutations — at the resource level, the path
//     level, and via path ancestors.
//   - Realistic Kubernetes upgrade scenarios — image bump + replica scale, port
//     addition, volume addition, multi-resource Add+Delete+Update batches.

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// computeMutationsHelper runs ComputeMutations with the K8s resource provider.
func computeMutationsHelper(t *testing.T, previous, modified string) api.ResourceMutationList {
	t.Helper()
	provider := k8skit.NewK8sResourceProvider()
	prev, err := gaby.ParseAll([]byte(previous))
	require.NoError(t, err)
	mod, err := gaby.ParseAll([]byte(modified))
	require.NoError(t, err)
	mutations, err := yamlkit.ComputeMutations(prev, mod, 1, provider)
	require.NoError(t, err)
	return mutations
}

// patchMutationsHelper runs PatchMutations against parsed YAML and returns the
// rendered output as a string.
func patchMutationsHelper(t *testing.T, target string, predicates, patch api.ResourceMutationList) string {
	t.Helper()
	provider := k8skit.NewK8sResourceProvider()
	parsed, err := gaby.ParseAll([]byte(target))
	require.NoError(t, err)
	out, _, err := yamlkit.PatchMutations(parsed, predicates, patch, nil, provider, nil)
	require.NoError(t, err)
	return out.String()
}

// findMutation returns the first ResourceMutation whose ResourceName (with or
// without scope) matches name. Fails the test if not found.
func findMutation(t *testing.T, mutations api.ResourceMutationList, name string) api.ResourceMutation {
	t.Helper()
	for _, m := range mutations {
		if string(m.Resource.ResourceName) == name ||
			string(m.Resource.ResourceNameWithoutScope) == name {
			return m
		}
	}
	t.Fatalf("no mutation found for resource %q", name)
	return api.ResourceMutation{}
}

// ---------------------------------------------------------------------------
// Resource-level rename heuristic
// ---------------------------------------------------------------------------

// TestRename_NameChange_SingleCandidate covers the simplest rename: a single
// resource of a given kind whose name changes. The exact-name match fails and
// the fuzzy branch picks up the only same-typed candidate.
func TestRename_NameChange_SingleCandidate(t *testing.T) {
	previous := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: old-name
  namespace: default
spec:
  replicas: 1
  selector:
    matchLabels:
      app: old-name
  template:
    spec:
      containers:
      - name: app
        image: nginx:1.19
`
	modified := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: new-name
  namespace: default
spec:
  replicas: 1
  selector:
    matchLabels:
      app: old-name
  template:
    spec:
      containers:
      - name: app
        image: nginx:1.19
`
	mutations := computeMutationsHelper(t, previous, modified)
	require.Len(t, mutations, 1, "rename should produce a single Update, not Add+Delete")
	m := mutations[0]
	assert.Equal(t, api.MutationTypeUpdate, m.ResourceMutationInfo.MutationType)
	assert.Equal(t, api.ResourceName("default/new-name"), m.Resource.ResourceName)
	assert.Contains(t, m.AliasesWithoutScopes, api.ResourceName("old-name"))
	assert.Contains(t, m.AliasesWithoutScopes, api.ResourceName("new-name"))
}

// TestRename_NameChange_MultipleCandidates exercises the matcher in the case
// where multiple same-typed resources exist and one is renamed. Exact-name
// match should pin the unchanged ones first; the renamed one should fall to
// fuzzy matching against the remaining unmatched previous doc.
func TestRename_NameChange_MultipleCandidates(t *testing.T) {
	previous := `apiVersion: v1
kind: ConfigMap
metadata:
  name: keep-me
  namespace: default
data:
  key: value-keep
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: rename-me
  namespace: default
data:
  key: value-rename
`
	modified := `apiVersion: v1
kind: ConfigMap
metadata:
  name: keep-me
  namespace: default
data:
  key: value-keep
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: renamed
  namespace: default
data:
  key: value-rename
`
	mutations := computeMutationsHelper(t, previous, modified)
	require.Len(t, mutations, 2)

	// keep-me matches by exact name — None or Update with empty path map.
	keep := findMutation(t, mutations, "keep-me")
	assert.Equal(t, api.MutationTypeNone, keep.ResourceMutationInfo.MutationType)

	// renamed matched fuzzily against rename-me.
	renamed := findMutation(t, mutations, "renamed")
	assert.Equal(t, api.MutationTypeUpdate, renamed.ResourceMutationInfo.MutationType)
	assert.Contains(t, renamed.AliasesWithoutScopes, api.ResourceName("rename-me"))
	assert.Contains(t, renamed.AliasesWithoutScopes, api.ResourceName("renamed"))
}

// TestRename_MergeIDOverridesNameMismatch — when both sides have a matching
// confighub.com/ResourceMergeID UUID, that match wins even with no name overlap
// or content overlap.
func TestRename_MergeIDOverridesNameMismatch(t *testing.T) {
	previous := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: alpha
  namespace: default
  annotations:
    confighub.com/ResourceMergeID: 11111111-1111-1111-1111-111111111111
spec:
  replicas: 1
  selector:
    matchLabels:
      app: alpha
  template:
    spec:
      containers:
      - name: c
        image: nginx:1.19
`
	modified := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: omega
  namespace: production
  annotations:
    confighub.com/ResourceMergeID: 11111111-1111-1111-1111-111111111111
spec:
  replicas: 99
  selector:
    matchLabels:
      app: omega
  template:
    spec:
      containers:
      - name: c
        image: redis:7
`
	mutations := computeMutationsHelper(t, previous, modified)
	require.Len(t, mutations, 1)
	m := mutations[0]
	assert.Equal(t, api.MutationTypeUpdate, m.ResourceMutationInfo.MutationType)
	assert.Equal(t, api.ResourceName("production/omega"), m.Resource.ResourceName)
	assert.Contains(t, m.AliasesWithoutScopes, api.ResourceName("alpha"))
}

// TestRename_NamespaceChange_NotARename — moving a resource to a different
// namespace while keeping the same name is matched by ResourceNameWithoutScope
// (exact match), not by the fuzzy branch. Both scoped names appear as aliases.
func TestRename_NamespaceChange_NotARename(t *testing.T) {
	previous := `apiVersion: v1
kind: ConfigMap
metadata:
  name: cfg
  namespace: ns-a
data:
  key: v1
`
	modified := `apiVersion: v1
kind: ConfigMap
metadata:
  name: cfg
  namespace: ns-b
data:
  key: v1
`
	mutations := computeMutationsHelper(t, previous, modified)
	require.Len(t, mutations, 1)
	m := mutations[0]
	assert.Equal(t, api.MutationTypeUpdate, m.ResourceMutationInfo.MutationType)
	assert.Contains(t, m.Aliases, api.ResourceName("ns-a/cfg"))
	assert.Contains(t, m.Aliases, api.ResourceName("ns-b/cfg"))
}

// TestRename_TypeChange — Deployment → StatefulSet keeping the same name. Pinned
// behavior: the matcher does treat similar-typed resources as the same one
// (Update with type changed). Documented for awareness; whether this is the
// right semantics is an open question.
func TestRename_TypeChange(t *testing.T) {
	previous := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: app
  namespace: default
spec:
  replicas: 1
`
	modified := `apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: app
  namespace: default
spec:
  replicas: 1
  serviceName: app
`
	mutations := computeMutationsHelper(t, previous, modified)
	require.Len(t, mutations, 1)
	m := mutations[0]
	assert.Equal(t, api.MutationTypeUpdate, m.ResourceMutationInfo.MutationType)
	assert.Equal(t, api.ResourceType("apps/v1/StatefulSet"), m.Resource.ResourceType)
}

// TestRename_NoOpNoChange — identical previous and modified produces a single
// None mutation, no aliases beyond the resource's own name.
func TestRename_NoOpNoChange(t *testing.T) {
	yamlData := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: app
  namespace: default
spec:
  replicas: 3
`
	mutations := computeMutationsHelper(t, yamlData, yamlData)
	require.Len(t, mutations, 1)
	m := mutations[0]
	assert.Equal(t, api.MutationTypeNone, m.ResourceMutationInfo.MutationType)
	assert.Empty(t, m.PathMutationMap)
}

// TestRename_DistinctTypesNotPaired — a Deployment in previous and an unrelated
// Service in modified produce Add + Delete, not a rename Update, because the
// types aren't similar.
func TestRename_DistinctTypesNotPaired(t *testing.T) {
	previous := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: a
  namespace: default
spec:
  replicas: 1
`
	modified := `apiVersion: v1
kind: Service
metadata:
  name: b
  namespace: default
spec:
  selector:
    app: b
  ports:
  - port: 80
`
	mutations := computeMutationsHelper(t, previous, modified)
	require.Len(t, mutations, 2)

	var hasAdd, hasDelete bool
	for _, m := range mutations {
		switch m.ResourceMutationInfo.MutationType {
		case api.MutationTypeAdd:
			hasAdd = true
			assert.Equal(t, api.ResourceType("v1/Service"), m.Resource.ResourceType)
		case api.MutationTypeDelete:
			hasDelete = true
			assert.Equal(t, api.ResourceType("apps/v1/Deployment"), m.Resource.ResourceType)
		}
	}
	assert.True(t, hasAdd, "expected an Add for the new Service")
	assert.True(t, hasDelete, "expected a Delete for the gone Deployment")
}

// ---------------------------------------------------------------------------
// Predicate filtering in PatchMutations
// ---------------------------------------------------------------------------

const predicateBaseDeployment = `apiVersion: apps/v1
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
        env:
        - name: ENV_A
          value: alpha
`

// patchUpdate builds a PatchMutations input that updates spec.replicas to 5 and
// the container image to nginx:1.20.
func patchUpdate() api.ResourceMutationList {
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
				Value:        "5\n",
			},
			"spec.template.spec.containers.?name=app;@0.image": api.MutationInfo{
				MutationType: api.MutationTypeUpdate,
				Index:        2,
				Predicate:    true,
				Value:        "nginx:1.20\n",
			},
		},
	}}
}

// TestPredicate_NilAppliesAllPaths — passing nil for predicates lets every
// patch through.
func TestPredicate_NilAppliesAllPaths(t *testing.T) {
	out := patchMutationsHelper(t, predicateBaseDeployment, nil, patchUpdate())
	assert.Contains(t, out, "replicas: 5")
	assert.Contains(t, out, "image: nginx:1.20")
}

// TestPredicate_PathFalseSkipsThatPath — when a predicate has Predicate=false
// at a specific path, the mutation at that path is skipped, while other paths
// are still applied.
func TestPredicate_PathFalseSkipsThatPath(t *testing.T) {
	predicates := api.ResourceMutationList{{
		Resource: api.ResourceInfo{
			ResourceType:             "apps/v1/Deployment",
			ResourceName:             "default/app",
			ResourceNameWithoutScope: "app",
			ResourceCategory:         "Kubernetes",
		},
		ResourceMutationInfo: api.MutationInfo{
			MutationType: api.MutationTypeUpdate,
			Index:        1,
			Predicate:    true,
		},
		PathMutationMap: api.MutationMap{
			"spec.replicas": api.MutationInfo{
				MutationType: api.MutationTypeUpdate,
				Index:        1,
				Predicate:    false, // block replicas
				Value:        "1\n",
			},
		},
	}}
	out := patchMutationsHelper(t, predicateBaseDeployment, predicates, patchUpdate())
	assert.Contains(t, out, "replicas: 1", "replicas should still be 1 (patch blocked)")
	assert.NotContains(t, out, "replicas: 5")
	assert.Contains(t, out, "image: nginx:1.20", "image patch should still apply")
}

// TestPredicate_AncestorFalseSkipsDescendants — Predicate=false at a parent
// path causes child path mutations to be skipped too.
func TestPredicate_AncestorFalseSkipsDescendants(t *testing.T) {
	predicates := api.ResourceMutationList{{
		Resource: api.ResourceInfo{
			ResourceType:             "apps/v1/Deployment",
			ResourceName:             "default/app",
			ResourceNameWithoutScope: "app",
			ResourceCategory:         "Kubernetes",
		},
		ResourceMutationInfo: api.MutationInfo{
			MutationType: api.MutationTypeUpdate,
			Index:        1,
			Predicate:    true,
		},
		PathMutationMap: api.MutationMap{
			// Block everything under spec.template.
			"spec.template": api.MutationInfo{
				MutationType: api.MutationTypeUpdate,
				Index:        1,
				Predicate:    false,
			},
		},
	}}
	out := patchMutationsHelper(t, predicateBaseDeployment, predicates, patchUpdate())
	assert.Contains(t, out, "image: nginx:1.19", "image patch under spec.template should be blocked")
	assert.Contains(t, out, "replicas: 5", "replicas (not under spec.template) should still apply")
}

// TestPredicate_ResourceLevelFalseSkipsResource — Predicate=false at the resource
// level causes the whole resource to be skipped by PatchMutations.
func TestPredicate_ResourceLevelFalseSkipsResource(t *testing.T) {
	predicates := api.ResourceMutationList{{
		Resource: api.ResourceInfo{
			ResourceType:             "apps/v1/Deployment",
			ResourceName:             "default/app",
			ResourceNameWithoutScope: "app",
			ResourceCategory:         "Kubernetes",
		},
		ResourceMutationInfo: api.MutationInfo{
			MutationType: api.MutationTypeUpdate,
			Index:        1,
			Predicate:    false, // block whole resource
		},
		PathMutationMap: api.MutationMap{},
	}}
	out := patchMutationsHelper(t, predicateBaseDeployment, predicates, patchUpdate())
	assert.Contains(t, out, "replicas: 1", "no patch should apply when resource-level predicate is false")
	assert.Contains(t, out, "image: nginx:1.19")
}

// TestPredicate_NoMatchingPredicateDoesntFilter — when the predicate list is
// nonempty but doesn't include the target resource, every path applies.
func TestPredicate_NoMatchingPredicateDoesntFilter(t *testing.T) {
	predicates := api.ResourceMutationList{{
		Resource: api.ResourceInfo{
			ResourceType:             "v1/ConfigMap",
			ResourceName:             "default/unrelated",
			ResourceNameWithoutScope: "unrelated",
			ResourceCategory:         "Kubernetes",
		},
		ResourceMutationInfo: api.MutationInfo{
			MutationType: api.MutationTypeUpdate,
			Index:        1,
			Predicate:    false,
		},
	}}
	out := patchMutationsHelper(t, predicateBaseDeployment, predicates, patchUpdate())
	assert.Contains(t, out, "replicas: 5")
	assert.Contains(t, out, "image: nginx:1.20")
}

// ---------------------------------------------------------------------------
// Realistic Kubernetes upgrade scenarios
// ---------------------------------------------------------------------------

// TestRealistic_ImageBumpAndReplicaScale — the most common upgrade: image bump
// plus replica scale on a single Deployment.
func TestRealistic_ImageBumpAndReplicaScale(t *testing.T) {
	prev := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: app
  namespace: default
spec:
  replicas: 3
  template:
    spec:
      containers:
      - name: app
        image: nginx:1.19
`
	mod := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: app
  namespace: default
spec:
  replicas: 6
  template:
    spec:
      containers:
      - name: app
        image: nginx:1.20
`
	mutations := computeMutationsHelper(t, prev, mod)
	require.Len(t, mutations, 1)
	m := mutations[0]
	assert.Equal(t, api.MutationTypeUpdate, m.ResourceMutationInfo.MutationType)

	out := patchMutationsHelper(t, prev, nil, mutations)
	assert.Contains(t, out, "replicas: 6")
	assert.Contains(t, out, "image: nginx:1.20")
}

// TestRealistic_AddPort — add a new container port via merge-keyed array.
func TestRealistic_AddPort(t *testing.T) {
	prev := `apiVersion: apps/v1
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
        ports:
        - containerPort: 80
          name: http
          protocol: TCP
`
	mod := `apiVersion: apps/v1
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
        ports:
        - containerPort: 80
          name: http
          protocol: TCP
        - containerPort: 9090
          name: metrics
          protocol: TCP
`
	mutations := computeMutationsHelper(t, prev, mod)
	out := patchMutationsHelper(t, prev, nil, mutations)
	assert.Contains(t, out, "containerPort: 9090")
	assert.Contains(t, out, "name: metrics")
	// http port still present
	assert.Contains(t, out, "containerPort: 80")
	assert.Contains(t, out, "name: http")
}

// TestRealistic_AddVolume — add a volume and a volumeMount in the same upgrade.
func TestRealistic_AddVolume(t *testing.T) {
	prev := `apiVersion: apps/v1
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
	mod := `apiVersion: apps/v1
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
        volumeMounts:
        - name: cfg
          mountPath: /etc/cfg
      volumes:
      - name: cfg
        configMap:
          name: cfg
`
	mutations := computeMutationsHelper(t, prev, mod)
	out := patchMutationsHelper(t, prev, nil, mutations)
	assert.Contains(t, out, "volumeMounts:")
	assert.Contains(t, out, "mountPath: /etc/cfg")
	assert.Contains(t, out, "volumes:")
	assert.Contains(t, out, "configMap:")
}

// TestRealistic_AddResourceAlongsideExisting — adding an HPA alongside an
// existing Deployment in a single upgrade. Verifies that single-resource Add
// mutations are emitted as separate ResourceMutation entries with type Add.
func TestRealistic_AddResourceAlongsideExisting(t *testing.T) {
	prev := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: app
  namespace: default
spec:
  replicas: 1
`
	mod := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: app
  namespace: default
spec:
  replicas: 1
---
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: app
  namespace: default
spec:
  minReplicas: 2
  maxReplicas: 10
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: app
`
	mutations := computeMutationsHelper(t, prev, mod)
	require.Len(t, mutations, 2)

	dep := findMutation(t, mutations, "app")
	// Both the Deployment and HPA are named "app"; pick the deployment by type.
	for _, m := range mutations {
		if m.Resource.ResourceType == "apps/v1/Deployment" {
			dep = m
		}
	}
	assert.Equal(t, api.MutationTypeNone, dep.ResourceMutationInfo.MutationType,
		"existing Deployment is unchanged")

	var hpa api.ResourceMutation
	for _, m := range mutations {
		if m.Resource.ResourceType == "autoscaling/v2/HorizontalPodAutoscaler" {
			hpa = m
		}
	}
	require.Equal(t, api.ResourceType("autoscaling/v2/HorizontalPodAutoscaler"), hpa.Resource.ResourceType)
	assert.Equal(t, api.MutationTypeAdd, hpa.ResourceMutationInfo.MutationType)

	// Apply and confirm both resources are present in output by re-parsing.
	out := patchMutationsHelper(t, prev, nil, mutations)
	parsed, err := gaby.ParseAll([]byte(out))
	require.NoError(t, err)
	assert.Len(t, parsed, 2, "both resources should appear in output")
	assert.Contains(t, out, "kind: HorizontalPodAutoscaler")
}

// TestRealistic_MultiResourceMixedChanges — Update one resource, Delete another,
// Add a third, all in one upgrade. Verifies ComputeMutations distinguishes them
// and PatchMutations applies the right mutation type to each.
func TestRealistic_MultiResourceMixedChanges(t *testing.T) {
	prev := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: app
  namespace: default
spec:
  replicas: 1
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: gone
  namespace: default
data:
  obsolete: "1"
`
	mod := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: app
  namespace: default
spec:
  replicas: 5
---
apiVersion: v1
kind: Service
metadata:
  name: app-svc
  namespace: default
spec:
  selector:
    app: app
  ports:
  - port: 80
`
	mutations := computeMutationsHelper(t, prev, mod)
	// Three mutations: Deployment Update, ConfigMap Delete, Service Add.
	require.Len(t, mutations, 3)

	var got = map[api.MutationType]int{}
	for _, m := range mutations {
		got[m.ResourceMutationInfo.MutationType]++
	}
	assert.Equal(t, 1, got[api.MutationTypeUpdate], "one Update (Deployment)")
	assert.Equal(t, 1, got[api.MutationTypeDelete], "one Delete (ConfigMap)")
	assert.Equal(t, 1, got[api.MutationTypeAdd], "one Add (Service)")

	out := patchMutationsHelper(t, prev, nil, mutations)
	assert.Contains(t, out, "replicas: 5")
	assert.Contains(t, out, "kind: Service")
	assert.NotContains(t, out, "kind: ConfigMap")
	assert.NotContains(t, out, "name: gone")
}

// TestRealistic_AddSecurityContext — the typical "default-pod-security-context"
// pattern: a function adds securityContext to existing containers.
func TestRealistic_AddSecurityContext(t *testing.T) {
	prev := `apiVersion: apps/v1
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
	mod := `apiVersion: apps/v1
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
        securityContext:
          runAsNonRoot: true
          readOnlyRootFilesystem: true
          allowPrivilegeEscalation: false
      securityContext:
        seccompProfile:
          type: RuntimeDefault
`
	mutations := computeMutationsHelper(t, prev, mod)
	out := patchMutationsHelper(t, prev, nil, mutations)
	assert.Contains(t, out, "runAsNonRoot: true")
	assert.Contains(t, out, "readOnlyRootFilesystem: true")
	assert.Contains(t, out, "seccompProfile:")
}

// TestRealistic_LabelAndAnnotationChurn — adding labels and annotations to an
// existing resource.
func TestRealistic_LabelAndAnnotationChurn(t *testing.T) {
	prev := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: app
  namespace: default
  labels:
    app: app
spec:
  replicas: 1
`
	mod := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: app
  namespace: default
  labels:
    app: app
    team: platform
    tier: backend
  annotations:
    deploy.example.com/owner: platform
    deploy.example.com/cost-center: "1234"
spec:
  replicas: 1
`
	mutations := computeMutationsHelper(t, prev, mod)
	out := patchMutationsHelper(t, prev, nil, mutations)
	assert.Contains(t, out, "team: platform")
	assert.Contains(t, out, "tier: backend")
	assert.Contains(t, out, "deploy.example.com/owner: platform")
	assert.Contains(t, out, "deploy.example.com/cost-center: \"1234\"")
}

// ---------------------------------------------------------------------------
// Conflicting leaf-level changes (three-way merge)
// ---------------------------------------------------------------------------
//
// These tests cover the canonical "both sides changed the same scalar" case at
// container resource limits/requests — high-traffic territory for production
// conflicts. They reuse runUpgrade from the init-container suite, which runs
// the full base / sourceEnd / target three-way merge through PatchMutations'
// mutationsToSubtract argument. Today these silently drop the source-side
// change in favor of the target's; the upcoming conflict-reporting PR will
// surface them as MutationConflict entries without changing the patched bytes.

const cpuMemBase = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: app
  namespace: default
spec:
  replicas: 1
  selector:
    matchLabels:
      app: app
  template:
    spec:
      containers:
      - name: app
        image: nginx:1.19
        resources:
          requests:
            cpu: 500m
            memory: 256Mi
          limits:
            cpu: 1000m
            memory: 512Mi
`

// containerResource returns the value at
// spec.template.spec.containers[0].resources.<bucket>.<field>.
func containerResource(t *testing.T, yamlBytes []byte, bucket, field string) string {
	t.Helper()
	docs, err := gaby.ParseAll(yamlBytes)
	require.NoError(t, err)
	node := docs[0].S("spec", "template", "spec", "containers").Children()[0].
		S("resources", bucket, field)
	require.NotNil(t, node, "resources.%s.%s missing", bucket, field)
	return node.Data().(string)
}

// TestConflict_CPURequest_TargetWins — both sides bump requests.cpu to
// different values. SubtractMutations preserves the target's change; the
// source's change is dropped silently (today). The patched bytes reflect
// only the target's value.
func TestConflict_CPURequest_TargetWins(t *testing.T) {
	source := strings.Replace(cpuMemBase, "cpu: 500m", "cpu: 750m", 1)
	target := strings.Replace(cpuMemBase, "cpu: 500m", "cpu: 600m", 1)

	out := runUpgrade(t, cpuMemBase, source, target)
	assert.Equal(t, "600m", containerResource(t, out, "requests", "cpu"),
		"target's requests.cpu wins over source's")
}

// TestConflict_MemoryLimit_TargetWins — both sides bump limits.memory to
// different values; target wins.
func TestConflict_MemoryLimit_TargetWins(t *testing.T) {
	source := strings.Replace(cpuMemBase, "memory: 512Mi", "memory: 1Gi", 1)
	target := strings.Replace(cpuMemBase, "memory: 512Mi", "memory: 768Mi", 1)

	out := runUpgrade(t, cpuMemBase, source, target)
	assert.Equal(t, "768Mi", containerResource(t, out, "limits", "memory"),
		"target's limits.memory wins over source's")
}

// TestConflict_NonOverlappingFields_BothApply — source changes requests.cpu,
// target changes limits.memory. No conflict; both end up applied.
func TestConflict_NonOverlappingFields_BothApply(t *testing.T) {
	source := strings.Replace(cpuMemBase, "cpu: 500m", "cpu: 750m", 1)
	target := strings.Replace(cpuMemBase, "memory: 512Mi", "memory: 768Mi", 1)

	out := runUpgrade(t, cpuMemBase, source, target)
	assert.Equal(t, "750m", containerResource(t, out, "requests", "cpu"),
		"source's requests.cpu applies (target didn't touch it)")
	assert.Equal(t, "768Mi", containerResource(t, out, "limits", "memory"),
		"target's limits.memory survives (source didn't touch it)")
}

// TestConflict_RequestsVsLimitsCPU_BothApply — source changes requests.cpu,
// target changes limits.cpu. Different sub-paths under resources; both apply.
func TestConflict_RequestsVsLimitsCPU_BothApply(t *testing.T) {
	source := strings.Replace(cpuMemBase, "cpu: 500m", "cpu: 750m", 1)   // requests.cpu
	target := strings.Replace(cpuMemBase, "cpu: 1000m", "cpu: 1500m", 1) // limits.cpu

	out := runUpgrade(t, cpuMemBase, source, target)
	assert.Equal(t, "750m", containerResource(t, out, "requests", "cpu"))
	assert.Equal(t, "1500m", containerResource(t, out, "limits", "cpu"))
}

// TestConflict_SourceRemovesResources_TargetCustomizedField — source removes
// the entire resources block; target had bumped requests.cpu independently.
// SubtractMutations now lets the source-side Delete through (the parent's
// removal wins over the child override) and reports the shadowed override
// as a DeleteShadowed conflict.
func TestConflict_SourceRemovesResources_TargetCustomizedField(t *testing.T) {
	// Source: the resources block is gone entirely.
	source := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: app
  namespace: default
spec:
  replicas: 1
  selector:
    matchLabels:
      app: app
  template:
    spec:
      containers:
      - name: app
        image: nginx:1.19
`
	target := strings.Replace(cpuMemBase, "cpu: 500m", "cpu: 750m", 1)

	out := runUpgrade(t, cpuMemBase, source, target)
	docs, err := gaby.ParseAll(out)
	require.NoError(t, err)
	resources := docs[0].S("spec", "template", "spec", "containers").
		Children()[0].S("resources")
	assert.Nil(t, resources, "resources block is removed (source-side Delete applied)")
}

// TestRealistic_RoundTrip_PatchOnSelfYieldsSameOutput — applying a unit's own
// computed mutations to itself should leave it unchanged. Pins idempotence
// for the "no-op upgrade" case.
func TestRealistic_RoundTrip_PatchOnSelfYieldsSameOutput(t *testing.T) {
	yamlData := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: app
  namespace: default
spec:
  replicas: 3
  template:
    spec:
      containers:
      - name: app
        image: nginx:1.19
        ports:
        - containerPort: 80
          name: http
`
	mutations := computeMutationsHelper(t, yamlData, yamlData)
	require.Len(t, mutations, 1)
	assert.Equal(t, api.MutationTypeNone, mutations[0].ResourceMutationInfo.MutationType)

	out := patchMutationsHelper(t, yamlData, nil, mutations)
	// Re-parse both sides and compare structurally to avoid YAML formatting
	// differences.
	a, err := gaby.ParseAll([]byte(yamlData))
	require.NoError(t, err)
	b, err := gaby.ParseAll([]byte(out))
	require.NoError(t, err)
	assert.Equal(t, a.String(), b.String())
}
