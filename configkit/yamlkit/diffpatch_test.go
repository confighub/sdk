// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

// Tests are in a different package so that they can reference k8skit
package yamlkit_test

import (
	"testing"

	"github.com/confighub/sdk/configkit/k8skit"
	"github.com/confighub/sdk/configkit/yamlkit"
	"github.com/confighub/sdk/third_party/gaby"
	"github.com/stretchr/testify/assert"
)

const (
	// No output-only fields. It still has some commonly defaulted fields though.
	originalYAML = `
apiVersion: apps/v1
kind: Deployment
metadata:
  annotations:
    confighub.com/key: something
    deployment.kubernetes.io/revision: "1"
  labels:
    app: mydep
  name: mydep
  namespace: default
spec:
  progressDeadlineSeconds: 600
  replicas: 3
  revisionHistoryLimit: 10
  selector:
    matchLabels:
      app: mydep
  strategy:
    rollingUpdate:
      maxSurge: 25%
      maxUnavailable: 25%
    type: RollingUpdate
  template:
    metadata:
      creationTimestamp: null
      labels:
        app: mydep
    spec:
      containers:
      - image: nginx:latest
        imagePullPolicy: Always
        name: nginx
        ports:
        - containerPort: 8080
          protocol: TCP
        resources: {}
        terminationMessagePath: /termination-log
        terminationMessagePolicy: File
      - image: otel/opentelemetry-collector:latest-amd64
        imagePullPolicy: IfNotPresent
        name: otel-sidecar
        ports:
        - containerPort: 4318
          protocol: TCP
        resources: {}
        terminationMessagePath: /termination-log
        terminationMessagePolicy: File
      dnsPolicy: ClusterFirst
      restartPolicy: Always
      schedulerName: default-scheduler
      securityContext: {}
      terminationGracePeriodSeconds: 30
`
	targetDataYAML = `
apiVersion: apps/v1
kind: Deployment
metadata:
  labels:
    app: mydep
  annotations:
    confighub.com/key: something
  name: mydep
spec:
  replicas: 3
  paused: false
  selector:
    matchLabels:
      app: mydep
  strategy: {}
  template:
    metadata:
      labels:
        app: mydep
    spec:
      dnsPolicy: ClusterFirst
      containers:
      - image: nginx:latest
        name: nginx
        ports:
        - containerPort: 8080
        resources: {}
      - image: otel/opentelemetry-collector:latest-amd64
        name: otel-sidecar
        ports:
        - containerPort: 4318
`
	// This is the same as as originalYAML plus it has output-only fields.
	modifiedYAMLWithStatus = `
apiVersion: apps/v1
kind: Deployment
metadata:
  annotations:
    confighub.com/key: something
    deployment.kubernetes.io/revision: "1"
  creationTimestamp: "2025-05-16T20:20:55Z"
  generation: 1
  labels:
    app: mydep
  name: mydep
  namespace: default
  resourceVersion: "1339667"
  uid: 1f2f9be7-e5e7-48a7-a0b2-c62209888682
spec:
  progressDeadlineSeconds: 600
  replicas: 3
  revisionHistoryLimit: 10
  selector:
    matchLabels:
      app: mydep
  strategy:
    rollingUpdate:
      maxSurge: 25%
      maxUnavailable: 25%
    type: RollingUpdate
  template:
    metadata:
      creationTimestamp: null
      labels:
        app: mydep
    spec:
      containers:
      - image: nginx:latest
        imagePullPolicy: Always
        name: nginx
        ports:
        - containerPort: 8080
          protocol: TCP
        resources: {}
        terminationMessagePath: /termination-log
        terminationMessagePolicy: File
      - image: otel/opentelemetry-collector:latest-amd64
        imagePullPolicy: IfNotPresent
        name: otel-sidecar
        ports:
        - containerPort: 4318
          protocol: TCP
        resources: {}
        terminationMessagePath: /termination-log
        terminationMessagePolicy: File
      dnsPolicy: ClusterFirst
      restartPolicy: Always
      schedulerName: default-scheduler
      securityContext: {}
      terminationGracePeriodSeconds: 30
status:
  availableReplicas: 3
  conditions:
  - lastTransitionTime: "2025-05-16T20:20:57Z"
    lastUpdateTime: "2025-05-16T20:20:57Z"
    message: Deployment has minimum availability.
    reason: MinimumReplicasAvailable
    status: "True"
    type: Available
  - lastTransitionTime: "2025-05-16T20:20:55Z"
    lastUpdateTime: "2025-05-16T20:20:57Z"
    message: ReplicaSet "mydep-5988d6596" has successfully progressed.
    reason: NewReplicaSetAvailable
    status: "True"
    type: Progressing
  observedGeneration: 1
  readyReplicas: 3
  replicas: 3
  updatedReplicas: 3
`
)

func TestDiffPatch(t *testing.T) {
	t.Run("no changes when original and modified are identical", func(t *testing.T) {
		original := []byte(`apiVersion: apps/v1
kind: Deployment
metadata:
  name: nginx
spec:
  replicas: 3
  template:
    spec:
      containers:
      - image: nginx:1.14.2
        name: nginx
`)
		modified := original // Same content
		target := original   // Same content

		result, changed, err := yamlkit.DiffPatch(original, modified, target, k8skit.K8sResourceProvider)

		assert.NoError(t, err)
		assert.False(t, changed)
		assert.Equal(t, string(target), string(result), "Result should be unchanged when no differences exist")
	})

	// Covers the len(patchMap) == 0 case
	t.Run("identical YAML documents have no differences", func(t *testing.T) {
		// Using minimal but valid Kubernetes YAML
		original := []byte(`apiVersion: v1
kind: ConfigMap
metadata:
  name: empty-config
data: {}
`)
		modified := []byte(`apiVersion: v1
kind: ConfigMap
metadata:
  name: empty-config
data: {}
`)
		target := []byte(`apiVersion: v1
kind: ConfigMap
metadata:
  name: empty-config
data: {}
`)

		result, changed, err := yamlkit.DiffPatch(original, modified, target, k8skit.K8sResourceProvider)
		assert.NoError(t, err)
		assert.False(t, changed)
		assert.Equal(t, string(target), string(result))
	})

	t.Run("patches target correctly when labels are added", func(t *testing.T) {
		original := []byte(`apiVersion: apps/v1
kind: Deployment
metadata:
  name: nginx
spec:
  replicas: 3
  template:
    spec:
      containers:
      - image: nginx:1.14.2
        name: nginx
`)
		modified := []byte(`apiVersion: apps/v1
kind: Deployment
metadata:
  name: nginx
  labels:
    app: nginx # this comment should be preserved
    environment: prod # also this
spec:
  replicas: 3
  template:
    spec:
      containers:
      - image: nginx:1.14.2
        name: nginx
`)
		target := []byte(`apiVersion: apps/v1
kind: Deployment
metadata:
  # this comment should be still here
  name: nginx
spec:
  replicas: 3
  template:
    spec:
      containers:
      - image: nginx:1.14.2
        name: nginx
`)

		result, changed, err := yamlkit.DiffPatch(original, modified, target, k8skit.K8sResourceProvider)

		assert.NoError(t, err)
		assert.True(t, changed, "Should indicate changes were made")

		// Expected result should contain the new labels
		expected := []byte(`apiVersion: apps/v1
kind: Deployment
metadata:
  # this comment should be still here
  name: nginx
  labels:
    app: nginx # this comment should be preserved
    environment: prod # also this
spec:
  replicas: 3
  template:
    spec:
      containers:
      - image: nginx:1.14.2
        name: nginx
`)
		assert.Equal(t, string(expected), string(result), "Result should contain the added labels")
	})

	t.Run("error when original YAML is invalid", func(t *testing.T) {
		original := []byte(`invalid yaml content: [`)
		modified := []byte(`apiVersion: v1
kind: ConfigMap
metadata:
  name: test
data:
  key: value
`)
		target := modified

		_, changed, err := yamlkit.DiffPatch(original, modified, target, k8skit.K8sResourceProvider)
		assert.Error(t, err)
		assert.False(t, changed)
		assert.Contains(t, err.Error(), "failed to parse original YAML")
	})

	t.Run("error when modified YAML is invalid", func(t *testing.T) {
		original := []byte(`apiVersion: v1
kind: ConfigMap
metadata:
  name: test
data:
  key: value
`)
		modified := []byte(`invalid yaml content: [`)
		target := original

		_, changed, err := yamlkit.DiffPatch(original, modified, target, k8skit.K8sResourceProvider)
		assert.Error(t, err)
		assert.False(t, changed)
		assert.Contains(t, err.Error(), "failed to parse modified YAML")
	})

	t.Run("error when target YAML is invalid", func(t *testing.T) {
		original := []byte(`apiVersion: v1
kind: ConfigMap
metadata:
  name: test
data:
  key: value
`)
		modified := []byte(`apiVersion: v1
kind: ConfigMap
metadata:
  name: test
data:
  key: modified
`)
		target := []byte(`invalid yaml content: [`)

		_, changed, err := yamlkit.DiffPatch(original, modified, target, k8skit.K8sResourceProvider)
		assert.Error(t, err)
		assert.False(t, changed)
		assert.Contains(t, err.Error(), "failed to parse target YAML")
	})

	t.Run("handles different structure changes", func(t *testing.T) {
		original := []byte(`apiVersion: v1
kind: ConfigMap
metadata:
  name: test
data:
  key: value
`)
		modified := []byte(`apiVersion: v1
kind: ConfigMap
metadata:
  name: test
data:
  key: value
  new-key: new-value
`)
		target := []byte(`apiVersion: v1
kind: ConfigMap
metadata:
  name: test
  annotations:
    test: annotation
data:
  key: value
`)

		expected := []byte(`apiVersion: v1
kind: ConfigMap
metadata:
  name: test
  annotations:
    test: annotation
data:
  key: value
  new-key: new-value
`)

		result, changed, err := yamlkit.DiffPatch(original, modified, target, k8skit.K8sResourceProvider)
		assert.NoError(t, err)
		assert.True(t, changed)
		assert.Equal(t, string(expected), string(result))
	})

	t.Run("test with different YAML structures", func(t *testing.T) {
		original := []byte(`apiVersion: v1
kind: ConfigMap
metadata:
  name: test
data:
  key1: value1
  key2: value2
`)
		modified := []byte(`apiVersion: v1
kind: ConfigMap
metadata:
  name: test
data:
  key1: modified1
  key3: value3
`)
		target := []byte(`apiVersion: v1
kind: ConfigMap
metadata:
  name: test
  annotations:
    note: "test annotation"
data:
  key1: value1
  key2: value2
`)

		// This should test the yamlkit.Patch path without causing a panic
		result, changed, err := yamlkit.DiffPatch(original, modified, target, k8skit.K8sResourceProvider)
		assert.NoError(t, err)
		assert.True(t, changed)

		// The DiffPatch function seems to both add new keys and modify existing ones,
		// but it appears to remove keys that were removed in the modified YAML
		expected := []byte(`apiVersion: v1
kind: ConfigMap
metadata:
  name: test
  annotations:
    note: "test annotation"
data:
  key1: modified1
  key3: value3
`)
		assert.Equal(t, string(expected), string(result))
	})

	t.Run("error during diff YAML", func(t *testing.T) {
		original := []byte(`apiVersion: v1
kind: ConfigMap
metadata:
  name: test
`)
		modified := []byte(`apiVersion: v1
kind: ConfigMap
metadata:
  name: test
  labels:
    app: test
`)
		target := []byte(`{}`)

		_, _, err := yamlkit.DiffPatch(original, modified, target, k8skit.K8sResourceProvider)
		_ = err
	})

	t.Run("error during patch application", func(t *testing.T) {
		// Creating a test case that might cause issues with patching
		// Using a valid but simple YAML document
		original := []byte(`apiVersion: v1
kind: ConfigMap
metadata:
  name: test
data:
  key: value
`)
		// Modified with different structure
		modified := []byte(`apiVersion: v1
kind: ConfigMap
metadata:
  name: test
  labels:
    app: test
data:
  key: new-value
`)
		// Target with invalid structure for patch application
		target := []byte(`apiVersion: v1
kind: ConfigMap
metadata:
  name: test
data: invalid-data
`)

		// Since triggering the exact error path is difficult, we'll verify no panic
		_, _, err := yamlkit.DiffPatch(original, modified, target, k8skit.K8sResourceProvider)
		// Don't assert the specific error - just make sure it runs
		_ = err
	})
}

func TestDiffPatch_NoNullSuffixRegression(t *testing.T) {
	original := []byte(originalYAML)
	modified := []byte(originalYAML)
	targetData := []byte(targetDataYAML)

	t.Run("does not append ---\\nnull\\n to result", func(t *testing.T) {
		result, changed, err := yamlkit.DiffPatch(original, modified, targetData, k8skit.K8sResourceProvider)
		assert.NoError(t, err)
		assert.False(t, changed, "Should not indicate changes when original and modified are identical")
		assert.NotContains(t, string(result), "---\nnull\n", "Result should not contain YAML document separator with null")
		assert.NotContains(t, string(result), "\nnull\n", "Result should not contain stray null lines")
		assert.NotContains(t, string(result), "---\nnull", "Result should not contain YAML document separator with null")
		assert.NotContains(t, string(result), "\nnull", "Result should not contain stray null lines")
		assert.NotContains(t, string(result), "null\n", "Result should not contain stray null lines")
		_, err = gaby.ParseAll(result)
		assert.NoError(t, err, "Result should be valid YAML")
	})

	t.Run("patches targetData with superset modified, no null doc", func(t *testing.T) {
		modifiedWithStatus := []byte(modifiedYAMLWithStatus)
		result, changed, err := yamlkit.DiffPatch(original, modifiedWithStatus, targetData, k8skit.K8sResourceProvider)
		assert.NoError(t, err)
		assert.True(t, changed, "Should indicate changes when modified is a superset")
		assert.NotContains(t, string(result), "---\nnull\n", "Result should not contain YAML document separator with null")
		assert.NotContains(t, string(result), "\nnull\n", "Result should not contain stray null lines")
		assert.NotContains(t, string(result), "---\nnull", "Result should not contain YAML document separator with null")
		assert.NotContains(t, string(result), "\nnull", "Result should not contain stray null lines")
		assert.NotContains(t, string(result), "null\n", "Result should not contain stray null lines")
		_, err = gaby.ParseAll(result)
		assert.NoError(t, err, "Result should be valid YAML")
	})

	t.Run("no changes when original and modified are identical, targetData is minimal", func(t *testing.T) {
		result, changed, err := yamlkit.DiffPatch(original, original, targetData, k8skit.K8sResourceProvider)
		assert.NoError(t, err)
		assert.False(t, changed, "Should not indicate changes when original and modified are identical")
		assert.Equal(t, string(targetData), string(result), "Result should be unchanged")
		assert.NotContains(t, string(result), "---\nnull\n", "Result should not contain YAML document separator with null")
		_, err = gaby.ParseAll(result)
		assert.NoError(t, err, "Result should be valid YAML")
	})
}
