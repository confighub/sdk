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

// End-to-end scenario sweep covering Kubernetes upgrade flows that exercise
// PatchMutations + SubtractMutations + ComputeMutations together. These tests
// complement the focused per-feature tests (rename detection, reorder pass,
// conflict reporting) by validating realistic patch shapes — sidecar
// injection, volume + volumeMount synchronization, mass label propagation,
// multi-resource adds, protection-blocked paths, empty-array edges, and
// non-Deployment resource types.

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// firstDoc parses yaml bytes and returns the first document.
func firstDoc(t *testing.T, yamlBytes []byte) *gaby.YamlDoc {
	t.Helper()
	docs, err := gaby.ParseAll(yamlBytes)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(docs), 1)
	return docs[0]
}

// scalarAt returns the scalar value at the given dot path on the doc.
func scalarAt(t *testing.T, doc *gaby.YamlDoc, path string) string {
	t.Helper()
	node := doc.Path(path)
	require.NotNilf(t, node, "no node at path %s", path)
	return node.Data().(string)
}

// containerNames returns the names from spec.template.spec.containers.
func containerNames(t *testing.T, doc *gaby.YamlDoc) []string {
	t.Helper()
	children := doc.S("spec", "template", "spec", "containers").Children()
	names := make([]string, 0, len(children))
	for _, c := range children {
		names = append(names, c.S("name").Data().(string))
	}
	return names
}

// ---------------------------------------------------------------------------
// Scenarios
// ---------------------------------------------------------------------------

// TestScenario_SidecarContainerInjection — source adds a sidecar container to
// containers[]. Target customized the main container's image. Both changes
// land: target's override survives, sidecar appears in source's intended
// position via the reorder pass.
func TestScenario_SidecarContainerInjection(t *testing.T) {
	base := `apiVersion: apps/v1
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
      - name: sidecar
        image: ghcr.io/example/sidecar:v1
`
	target := `apiVersion: apps/v1
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
        image: nginx:1.20
`
	out := runUpgrade(t, base, source, target)
	doc := firstDoc(t, out)
	assert.Equal(t, []string{"app", "sidecar"}, containerNames(t, doc))
	assert.Equal(t, "nginx:1.20",
		scalarAt(t, doc, "spec.template.spec.containers.0.image"),
		"target's image override survives")
	assert.Equal(t, "ghcr.io/example/sidecar:v1",
		scalarAt(t, doc, "spec.template.spec.containers.1.image"),
		"sidecar lands at index 1 per source order")
}

// TestScenario_VolumeAndVolumeMountSynchronized — source adds a volume and a
// matching volumeMount in the same upgrade. Target had its own env-var
// addition on the container. Both source's additions and target's
// customization end up in the patched output.
func TestScenario_VolumeAndVolumeMountSynchronized(t *testing.T) {
	base := `apiVersion: apps/v1
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
        volumeMounts:
        - name: cfg
          mountPath: /etc/cfg
      volumes:
      - name: cfg
        configMap:
          name: cfg
`
	target := `apiVersion: apps/v1
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
        env:
        - name: TEAM_OWNED
          value: "true"
`
	out := runUpgrade(t, base, source, target)
	doc := firstDoc(t, out)
	container := doc.S("spec", "template", "spec", "containers").Children()[0]
	// Source's volumeMount applied.
	mounts := container.S("volumeMounts").Children()
	require.Len(t, mounts, 1)
	assert.Equal(t, "cfg", mounts[0].S("name").Data())
	assert.Equal(t, "/etc/cfg", mounts[0].S("mountPath").Data())
	// Source's volume applied.
	volumes := doc.S("spec", "template", "spec", "volumes").Children()
	require.Len(t, volumes, 1)
	assert.Equal(t, "cfg", volumes[0].S("name").Data())
	assert.Equal(t, "cfg", volumes[0].S("configMap", "name").Data())
	// Target's env addition survives.
	envs := container.S("env").Children()
	require.Len(t, envs, 1)
	assert.Equal(t, "TEAM_OWNED", envs[0].S("name").Data())
	assert.Equal(t, "true", envs[0].S("value").Data())
}

// TestScenario_MassLabelPropagation — source adds the same label to a
// Deployment, a Service, and a ConfigMap in one upgrade. All three resources
// get the new label.
func TestScenario_MassLabelPropagation(t *testing.T) {
	base := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: app
  namespace: default
  labels:
    app: app
spec:
  replicas: 1
---
apiVersion: v1
kind: Service
metadata:
  name: app
  namespace: default
  labels:
    app: app
spec:
  selector:
    app: app
  ports:
  - port: 80
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: app-config
  namespace: default
  labels:
    app: app
data:
  key: value
`
	source := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: app
  namespace: default
  labels:
    app: app
    team: platform
spec:
  replicas: 1
---
apiVersion: v1
kind: Service
metadata:
  name: app
  namespace: default
  labels:
    app: app
    team: platform
spec:
  selector:
    app: app
  ports:
  - port: 80
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: app-config
  namespace: default
  labels:
    app: app
    team: platform
data:
  key: value
`
	out := runUpgrade(t, base, source, base)
	docs, err := gaby.ParseAll(out)
	require.NoError(t, err)
	require.Len(t, docs, 3)
	for _, doc := range docs {
		assert.Equal(t, "platform", doc.S("metadata", "labels", "team").Data(),
			"team: platform label propagated to %s", doc.S("kind").Data())
	}
}

// TestScenario_ServicePortAddition — Service.spec.ports is a merge-keyed
// array (merge key: port). Adding a new port preserves any target-side
// customization on the existing port.
func TestScenario_ServicePortAddition(t *testing.T) {
	base := `apiVersion: v1
kind: Service
metadata:
  name: app
  namespace: default
spec:
  selector:
    app: app
  ports:
  - port: 80
    name: http
    protocol: TCP
`
	source := `apiVersion: v1
kind: Service
metadata:
  name: app
  namespace: default
spec:
  selector:
    app: app
  ports:
  - port: 80
    name: http
    protocol: TCP
  - port: 9090
    name: metrics
    protocol: TCP
`
	target := `apiVersion: v1
kind: Service
metadata:
  name: app
  namespace: default
spec:
  selector:
    app: app
  ports:
  - port: 80
    name: http
    protocol: TCP
    targetPort: 8080
`
	out := runUpgrade(t, base, source, target)
	doc := firstDoc(t, out)
	ports := doc.S("spec", "ports").Children()
	require.Len(t, ports, 2)
	// Target's targetPort customization preserved.
	assert.Equal(t, 8080, doc.S("spec", "ports").Children()[0].S("targetPort").Data())
	// New port added.
	names := []string{
		ports[0].S("name").Data().(string),
		ports[1].S("name").Data().(string),
	}
	assert.Contains(t, names, "http")
	assert.Contains(t, names, "metrics")
}

// TestScenario_NetworkPolicyAddedToExistingResources — source introduces a
// brand-new NetworkPolicy alongside an existing Deployment. The Deployment is
// unchanged; the NetworkPolicy appears as a fresh document in the output.
func TestScenario_NetworkPolicyAddedToExistingResources(t *testing.T) {
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
  replicas: 1
---
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: app-deny-all
  namespace: default
spec:
  podSelector:
    matchLabels:
      app: app
  policyTypes:
  - Ingress
  - Egress
`
	out := runUpgrade(t, base, source, base)
	docs, err := gaby.ParseAll(out)
	require.NoError(t, err)
	require.Len(t, docs, 2, "Deployment + NetworkPolicy")

	var hasNetworkPolicy bool
	for _, doc := range docs {
		if doc.S("kind").Data().(string) == "NetworkPolicy" {
			hasNetworkPolicy = true
			assert.Equal(t, "app-deny-all", doc.S("metadata", "name").Data())
		}
	}
	assert.True(t, hasNetworkPolicy)
}

// TestScenario_ProtectionBlocksUpgradePathButOthersApply — a realistic
// "approve some, block others" flow. User has protection that allow image
// changes but block replicas changes; source bumps both. Replicas stays;
// image updates.
func TestScenario_ProtectionBlocksUpgradePathButOthersApply(t *testing.T) {
	provider := k8skit.NewK8sResourceProvider()
	target := `apiVersion: apps/v1
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
	parsed, err := gaby.ParseAll([]byte(target))
	require.NoError(t, err)

	patch := api.ResourceMutationList{{
		Resource: api.ResourceInfo{
			ResourceType:             "apps/v1/Deployment",
			ResourceName:             "default/app",
			ResourceNameWithoutScope: "app",
			ResourceCategory:         "Kubernetes",
		},
		ResourceMutationInfo: api.MutationInfo{
			MutationType: api.MutationTypeUpdate,
		},
		PathMutationMap: api.MutationMap{
			"spec.replicas": api.MutationInfo{
				MutationType: api.MutationTypeUpdate, Value: "9\n",
			},
			"spec.template.spec.containers.?name=app;@0.image": api.MutationInfo{
				MutationType: api.MutationTypeUpdate, Value: "nginx:1.20\n",
			},
		},
	}}
	protection := api.ResourceMutationList{{
		Resource: api.ResourceInfo{
			ResourceType:             "apps/v1/Deployment",
			ResourceName:             "default/app",
			ResourceNameWithoutScope: "app",
			ResourceCategory:         "Kubernetes",
		},
		ResourceMutationInfo: api.MutationInfo{MutationType: api.MutationTypeUpdate},
		PathMutationMap: api.MutationMap{
			"spec.replicas": api.MutationInfo{Protected: true}, // block replicas
		},
	}}

	out, conflicts, err := yamlkit.PatchMutations(parsed, protection, patch, nil, provider, nil)
	require.NoError(t, err)
	doc := firstDoc(t, []byte(out.String()))
	assert.Equal(t, 3, doc.S("spec", "replicas").Data(), "replicas blocked, stays at 3")
	assert.Equal(t, "nginx:1.20",
		doc.S("spec", "template", "spec", "containers").Children()[0].S("image").Data(),
		"image change applies")

	// Conflict reported for the blocked path.
	var foundConflict bool
	for _, c := range conflicts {
		if c.Reason == api.ConflictReasonProtectedPath && c.Path == "spec.replicas" {
			foundConflict = true
		}
	}
	assert.True(t, foundConflict, "blocked replicas reported as conflict")
}

// TestScenario_EmptyToPopulatedInitContainers — target has no initContainers
// at all. Source introduces them. They appear in source's order.
func TestScenario_EmptyToPopulatedInitContainers(t *testing.T) {
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
`
	source := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: app
  namespace: default
spec:
  template:
    spec:
      initContainers:
      - name: db-init
        image: ghcr.io/example/init:v1
      - name: db-migrate
        image: ghcr.io/example/migrate:v1
      containers:
      - name: app
        image: nginx:1.19
`
	out := runUpgrade(t, base, source, base)
	doc := firstDoc(t, out)
	inits := doc.S("spec", "template", "spec", "initContainers").Children()
	require.Len(t, inits, 2)
	assert.Equal(t, "db-init", inits[0].S("name").Data())
	assert.Equal(t, "db-migrate", inits[1].S("name").Data())
}

// TestScenario_StatefulSetUpgrade — non-Deployment resource. Same merge-key
// shapes for containers/env, plus serviceName change. Target's image
// override on the main container survives.
func TestScenario_StatefulSetUpgrade(t *testing.T) {
	base := `apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: db
  namespace: default
spec:
  serviceName: db
  replicas: 3
  selector:
    matchLabels:
      app: db
  template:
    spec:
      containers:
      - name: db
        image: postgres:14
`
	source := `apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: db
  namespace: default
spec:
  serviceName: db-headless
  replicas: 3
  selector:
    matchLabels:
      app: db
  template:
    spec:
      containers:
      - name: db
        image: postgres:15
`
	target := `apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: db
  namespace: default
spec:
  serviceName: db
  replicas: 5
  selector:
    matchLabels:
      app: db
  template:
    spec:
      containers:
      - name: db
        image: postgres:14-custom
`
	out := runUpgrade(t, base, source, target)
	doc := firstDoc(t, out)
	assert.Equal(t, "db-headless", doc.S("spec", "serviceName").Data(),
		"source's serviceName change applies")
	assert.Equal(t, 5, doc.S("spec", "replicas").Data(),
		"target's replicas override survives")
	assert.Equal(t, "postgres:14-custom",
		doc.S("spec", "template", "spec", "containers").Children()[0].S("image").Data(),
		"target's image override survives")
}

// TestScenario_ConfigMapDataUpdate — ConfigMap.data is a map, not an array.
// Source adds a key, target customized another; both apply.
func TestScenario_ConfigMapDataUpdate(t *testing.T) {
	base := `apiVersion: v1
kind: ConfigMap
metadata:
  name: app-cfg
  namespace: default
data:
  shared: "v1"
`
	source := `apiVersion: v1
kind: ConfigMap
metadata:
  name: app-cfg
  namespace: default
data:
  shared: "v2"
  log-level: "debug"
`
	target := `apiVersion: v1
kind: ConfigMap
metadata:
  name: app-cfg
  namespace: default
data:
  shared: "v1"
  custom: "team-set"
`
	out := runUpgrade(t, base, source, target)
	doc := firstDoc(t, out)
	data := doc.S("data")
	assert.Equal(t, "v2", data.S("shared").Data(), "source's shared update applies")
	assert.Equal(t, "debug", data.S("log-level").Data(), "source's new key applies")
	assert.Equal(t, "team-set", data.S("custom").Data(), "target's key survives")
}

// TestScenario_DaemonSetUpgrade — DaemonSet has a similar PodTemplateSpec
// to Deployment. Verifies container changes apply correctly.
func TestScenario_DaemonSetUpgrade(t *testing.T) {
	base := `apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: log-collector
  namespace: kube-system
spec:
  selector:
    matchLabels:
      app: log-collector
  template:
    spec:
      containers:
      - name: collector
        image: fluent/fluentd:v1.16
`
	source := `apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: log-collector
  namespace: kube-system
spec:
  selector:
    matchLabels:
      app: log-collector
  template:
    spec:
      containers:
      - name: collector
        image: fluent/fluentd:v1.17
        resources:
          requests:
            cpu: 100m
            memory: 128Mi
`
	out := runUpgrade(t, base, source, base)
	doc := firstDoc(t, out)
	c := doc.S("spec", "template", "spec", "containers").Children()[0]
	assert.Equal(t, "fluent/fluentd:v1.17", c.S("image").Data())
	assert.Equal(t, "100m", c.S("resources", "requests", "cpu").Data())
}

// TestScenario_ResourceRemovedFromMultiResourceUpgrade — source drops one
// of three resources from a multi-resource bundle. Target had no overrides
// on the dropped resource, so the Delete cleanly applies.
func TestScenario_ResourceRemovedFromMultiResourceUpgrade(t *testing.T) {
	base := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: app
  namespace: default
spec:
  replicas: 1
---
apiVersion: v1
kind: Service
metadata:
  name: app
  namespace: default
spec:
  selector:
    app: app
  ports:
  - port: 80
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: legacy-cfg
  namespace: default
data:
  obsolete: "1"
`
	// Source drops the ConfigMap.
	source := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: app
  namespace: default
spec:
  replicas: 1
---
apiVersion: v1
kind: Service
metadata:
  name: app
  namespace: default
spec:
  selector:
    app: app
  ports:
  - port: 80
`
	out := runUpgrade(t, base, source, base)
	docs, err := gaby.ParseAll(out)
	require.NoError(t, err)
	require.Len(t, docs, 2, "Deployment + Service after Delete of ConfigMap")
	for _, doc := range docs {
		assert.NotEqual(t, "ConfigMap", doc.S("kind").Data())
	}
}

// TestScenario_KitchenSink — combined upgrade exercising rename + reorder +
// target override + nested env change in one pass. Spans multiple features
// to confirm they compose correctly.
//
// Setup:
//   - base has two initContainers [db-init, db-migrate], each with two env
//     vars [ENV_A, ENV_B].
//   - source renames db-init to db-init-v2, swaps to [db-migrate, db-init-v2]
//     order, and changes ENV_B's value on the renamed container.
//   - target customized db-init's image and added ENV_TARGET to db-init.
//
// Expected:
//   - db-init's element renamed to db-init-v2 (rename applied via alias).
//   - target's image override survives (path under previous key, no source
//     conflict — source didn't touch image).
//   - target's ENV_TARGET addition survives.
//   - source's ENV_B change applies (target didn't touch it).
//   - Final order is [db-migrate, db-init-v2] per source's ArrayOrders.
func TestScenario_KitchenSink(t *testing.T) {
	base := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: app
  namespace: default
  annotations:
    confighub.com/ResourceMergeID: 33333333-3333-3333-3333-333333333333
spec:
  replicas: 1
  selector:
    matchLabels:
      app: app
  template:
    spec:
      initContainers:
      - name: db-init
        image: ghcr.io/example/init:v1
        env:
        - name: ENV_A
          value: alpha
        - name: ENV_B
          value: bravo
      - name: db-migrate
        image: ghcr.io/example/migrate:v1
        env:
        - name: ENV_A
          value: alpha
        - name: ENV_B
          value: bravo
      containers:
      - name: app
        image: nginx:1.19
`
	source := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: app
  namespace: default
  annotations:
    confighub.com/ResourceMergeID: 33333333-3333-3333-3333-333333333333
spec:
  replicas: 1
  selector:
    matchLabels:
      app: app
  template:
    spec:
      initContainers:
      - name: db-migrate
        image: ghcr.io/example/migrate:v1
        env:
        - name: ENV_A
          value: alpha
        - name: ENV_B
          value: bravo
      - name: db-init-v2
        image: ghcr.io/example/init:v1
        env:
        - name: ENV_A
          value: alpha
        - name: ENV_B
          value: BRAVO_NEW
      containers:
      - name: app
        image: nginx:1.19
`
	target := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: app
  namespace: default
  annotations:
    confighub.com/ResourceMergeID: 33333333-3333-3333-3333-333333333333
spec:
  replicas: 1
  selector:
    matchLabels:
      app: app
  template:
    spec:
      initContainers:
      - name: db-init
        image: ghcr.io/example/init:custom
        env:
        - name: ENV_A
          value: alpha
        - name: ENV_B
          value: bravo
        - name: ENV_TARGET
          value: target-only
      - name: db-migrate
        image: ghcr.io/example/migrate:v1
        env:
        - name: ENV_A
          value: alpha
        - name: ENV_B
          value: bravo
      containers:
      - name: app
        image: nginx:1.19
`
	out := runUpgrade(t, base, source, target)
	doc := firstDoc(t, out)

	inits := doc.S("spec", "template", "spec", "initContainers").Children()
	names := []string{}
	for _, c := range inits {
		names = append(names, c.S("name").Data().(string))
	}
	assert.Equal(t, []string{"db-migrate", "db-init-v2"}, names,
		"reorder + rename: db-init renamed to db-init-v2 in second slot")

	// db-init-v2 (the renamed element) is the SECOND init container.
	dbInitV2 := inits[1]
	assert.Equal(t, "ghcr.io/example/init:custom", dbInitV2.S("image").Data(),
		"target's image override carried forward to renamed element")

	// Target's ENV_TARGET addition survives.
	envNames := []string{}
	envValues := map[string]string{}
	for _, e := range dbInitV2.S("env").Children() {
		n := e.S("name").Data().(string)
		envNames = append(envNames, n)
		envValues[n] = e.S("value").Data().(string)
	}
	assert.Contains(t, envNames, "ENV_TARGET", "target-only env var survives the rename")
	assert.Equal(t, "target-only", envValues["ENV_TARGET"])
	// Source's ENV_B change applies.
	assert.Equal(t, "BRAVO_NEW", envValues["ENV_B"], "source's ENV_B change applies")
	// ENV_A unchanged (neither side touched its value beyond the rename).
	assert.Equal(t, "alpha", envValues["ENV_A"])
}

// TestScenario_RoundTripStable — an upgrade against an unchanged target where
// source equals base produces no changes. Idempotence baseline for the whole
// pipeline.
func TestScenario_RoundTripStable(t *testing.T) {
	yamlData := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: app
  namespace: default
spec:
  replicas: 3
  template:
    spec:
      initContainers:
      - name: db-init
        image: ghcr.io/example/init:v1
      containers:
      - name: app
        image: nginx:1.19
        ports:
        - containerPort: 80
          name: http
`
	out := runUpgrade(t, yamlData, yamlData, yamlData)
	// Re-parse and compare structurally.
	a, err := gaby.ParseAll([]byte(yamlData))
	require.NoError(t, err)
	b, err := gaby.ParseAll(out)
	require.NoError(t, err)
	assert.Equal(t, strings.TrimSpace(a.String()), strings.TrimSpace(b.String()))
}
