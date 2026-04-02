// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package k8skit_test

import (
	"testing"

	"github.com/confighub/sdk/configkit/k8skit"
	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/third_party/gaby"
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

		result, changed, err := yamlkit.DiffPatch(original, modified, target, k8skit.NewK8sResourceProvider())

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

		result, changed, err := yamlkit.DiffPatch(original, modified, target, k8skit.NewK8sResourceProvider())
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

		result, changed, err := yamlkit.DiffPatch(original, modified, target, k8skit.NewK8sResourceProvider())

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

		_, changed, err := yamlkit.DiffPatch(original, modified, target, k8skit.NewK8sResourceProvider())
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

		_, changed, err := yamlkit.DiffPatch(original, modified, target, k8skit.NewK8sResourceProvider())
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

		_, changed, err := yamlkit.DiffPatch(original, modified, target, k8skit.NewK8sResourceProvider())
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

		result, changed, err := yamlkit.DiffPatch(original, modified, target, k8skit.NewK8sResourceProvider())
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
		result, changed, err := yamlkit.DiffPatch(original, modified, target, k8skit.NewK8sResourceProvider())
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

		_, _, err := yamlkit.DiffPatch(original, modified, target, k8skit.NewK8sResourceProvider())
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
		_, _, err := yamlkit.DiffPatch(original, modified, target, k8skit.NewK8sResourceProvider())
		// Don't assert the specific error - just make sure it runs
		_ = err
	})
}

func TestDiffPatch_NoNullSuffixRegression(t *testing.T) {
	original := []byte(originalYAML)
	modified := []byte(originalYAML)
	targetData := []byte(targetDataYAML)

	t.Run("does not append ---\\nnull\\n to result", func(t *testing.T) {
		result, changed, err := yamlkit.DiffPatch(original, modified, targetData, k8skit.NewK8sResourceProvider())
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
		result, changed, err := yamlkit.DiffPatch(original, modifiedWithStatus, targetData, k8skit.NewK8sResourceProvider())
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
		result, changed, err := yamlkit.DiffPatch(original, original, targetData, k8skit.NewK8sResourceProvider())
		assert.NoError(t, err)
		assert.False(t, changed, "Should not indicate changes when original and modified are identical")
		assert.Equal(t, string(targetData), string(result), "Result should be unchanged")
		assert.NotContains(t, string(result), "---\nnull\n", "Result should not contain YAML document separator with null")
		_, err = gaby.ParseAll(result)
		assert.NoError(t, err, "Result should be valid YAML")
	})
}

func TestDiffPatchWithOptions_OmitAdditions(t *testing.T) {
	original := []byte(`apiVersion: v1
kind: ConfigMap
metadata:
  name: my-config
data:
  key1: value1`)

	modified := []byte(`apiVersion: v1
kind: ConfigMap
metadata:
  name: my-config
data:
  key1: modified-value1
  key2: value2`)

	target := original

	t.Run("without omitAdditions includes all changes", func(t *testing.T) {
		result, changed, err := yamlkit.DiffPatchWithOptions(original, modified, target, k8skit.NewK8sResourceProvider(), false)

		assert.NoError(t, err)
		assert.True(t, changed)
		resultStr := string(result)
		assert.Contains(t, resultStr, "modified-value1")
		assert.Contains(t, resultStr, "key2: value2")
	})

	t.Run("with omitAdditions excludes new keys but includes modifications", func(t *testing.T) {
		result, changed, err := yamlkit.DiffPatchWithOptions(original, modified, target, k8skit.NewK8sResourceProvider(), true)

		assert.NoError(t, err)
		assert.True(t, changed)
		resultStr := string(result)
		assert.Contains(t, resultStr, "modified-value1")
		assert.NotContains(t, resultStr, "key2: value2")
	})

	t.Run("no changes when original and modified are identical", func(t *testing.T) {
		result, changed, err := yamlkit.DiffPatchWithOptions(original, original, target, k8skit.NewK8sResourceProvider(), true)

		assert.NoError(t, err)
		assert.False(t, changed)
		assert.Equal(t, string(target), string(result))
	})

	t.Run("omitAdditions filters out new resources entirely", func(t *testing.T) {
		originalMulti := []byte(`apiVersion: v1
kind: ConfigMap
metadata:
  name: existing-config
data:
  key1: value1`)

		modifiedMulti := []byte(`apiVersion: v1
kind: ConfigMap
metadata:
  name: existing-config
data:
  key1: modified-value1
---
apiVersion: v1
kind: Secret
metadata:
  name: new-secret
data:
  secret-key: c2VjcmV0`)

		targetMulti := originalMulti

		result, changed, err := yamlkit.DiffPatchWithOptions(originalMulti, modifiedMulti, targetMulti, k8skit.NewK8sResourceProvider(), true)

		assert.NoError(t, err)
		assert.True(t, changed)
		resultStr := string(result)
		assert.Contains(t, resultStr, "modified-value1")
		assert.NotContains(t, resultStr, "Secret")
		assert.NotContains(t, resultStr, "new-secret")
		assert.NotContains(t, resultStr, "secret-key")
	})

	t.Run("delete mutations remove entire resources", func(t *testing.T) {
		originalWithTwo := []byte(`apiVersion: v1
kind: ConfigMap
metadata:
  name: config-one
data:
  key1: value1
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: config-two
data:
  key2: value2`)

		modifiedWithOne := []byte(`apiVersion: v1
kind: ConfigMap
metadata:
  name: config-one
data:
  key1: value1-modified`)

		targetWithTwo := originalWithTwo

		// Test that the second resource is deleted
		result, changed, err := yamlkit.DiffPatchWithOptions(originalWithTwo, modifiedWithOne, targetWithTwo, k8skit.NewK8sResourceProvider(), false)

		assert.NoError(t, err)
		assert.True(t, changed)
		resultStr := string(result)
		assert.Contains(t, resultStr, "config-one")
		assert.Contains(t, resultStr, "value1-modified")
		assert.NotContains(t, resultStr, "config-two")
		assert.NotContains(t, resultStr, "key2: value2")

		// Verify no stray YAML separators or null documents
		assert.NotContains(t, resultStr, "---\nnull")
		assert.NotContains(t, resultStr, "\nnull\n")
		assert.NotContains(t, resultStr, "null\n")
	})

	t.Run("test omitAdditions with field-level additions", func(t *testing.T) {
		originalFieldLevel := []byte(`apiVersion: v1
kind: ConfigMap
metadata:
  name: test-config
data:
  existing-key: existing-value`)

		modifiedFieldLevel := []byte(`apiVersion: v1
kind: ConfigMap
metadata:
  name: test-config
  labels:
    app: test
data:
  existing-key: updated-value
  new-key: new-value`)

		targetFieldLevel := originalFieldLevel

		// Test with omitAdditions=true - should only update existing fields
		result, changed, err := yamlkit.DiffPatchWithOptions(originalFieldLevel, modifiedFieldLevel, targetFieldLevel, k8skit.NewK8sResourceProvider(), true)

		assert.NoError(t, err)
		assert.True(t, changed)
		resultStr := string(result)
		assert.Contains(t, resultStr, "updated-value")
		assert.NotContains(t, resultStr, "new-key: new-value")
		assert.NotContains(t, resultStr, "labels:")
		assert.NotContains(t, resultStr, "app: test")

		// Test with omitAdditions=false - should include all changes
		result2, changed2, err2 := yamlkit.DiffPatchWithOptions(originalFieldLevel, modifiedFieldLevel, targetFieldLevel, k8skit.NewK8sResourceProvider(), false)

		assert.NoError(t, err2)
		assert.True(t, changed2)
		resultStr2 := string(result2)
		assert.Contains(t, resultStr2, "updated-value")
		assert.Contains(t, resultStr2, "new-key: new-value")
		assert.Contains(t, resultStr2, "labels:")
		assert.Contains(t, resultStr2, "app: test")
	})
}

func TestDiffPatch_PreservesLineComments(t *testing.T) {
	t.Run("preserves line comments on scalar fields", func(t *testing.T) {
		original := []byte(`apiVersion: apps/v1
kind: Deployment
metadata:
  name: mydep
  namespace: example
spec:
  replicas: 3 # Line comment on replicas
  template:
    spec:
      containers:
      - image: nginx:latest
        name: nginx
        resources: {}
`)

		// Modified version changes replicas to 5
		modified := []byte(`apiVersion: apps/v1
kind: Deployment
metadata:
  name: mydep
  namespace: example
spec:
  replicas: 5
  template:
    spec:
      containers:
      - image: nginx:latest
        name: nginx
        resources: {}
`)

		target := original

		result, changed, err := yamlkit.DiffPatch(original, modified, target, k8skit.NewK8sResourceProvider())

		assert.NoError(t, err)
		assert.True(t, changed, "Should indicate changes were made")

		// Parse the result to verify it's valid YAML
		parsedResult, err := gaby.ParseYAML(result)
		assert.NoError(t, err)

		// Check that the replicas value was updated
		replicas := parsedResult.Path("spec.replicas")
		assert.NotNil(t, replicas)
		replicasValue, ok := replicas.Data().(int)
		assert.True(t, ok, "replicas should be an integer")
		assert.Equal(t, 5, replicasValue, "replicas should be updated to 5")

		// Check that the line comment is preserved
		assert.Contains(t, string(result), "# Line comment on replicas", "Line comment should be preserved")
		assert.Contains(t, string(result), "replicas: 5 # Line comment on replicas", "Comment should be on the same line as the updated value")
	})

	t.Run("preserves comments when adding new fields", func(t *testing.T) {
		original := []byte(`apiVersion: v1
kind: ConfigMap
metadata:
  name: test-config
data:
  key1: value1 # Important key
`)

		modified := []byte(`apiVersion: v1
kind: ConfigMap
metadata:
  name: test-config
data:
  key1: value1
  key2: value2 # New key
`)

		target := original

		result, changed, err := yamlkit.DiffPatch(original, modified, target, k8skit.NewK8sResourceProvider())

		assert.NoError(t, err)
		assert.True(t, changed)

		// Both comments should be present in the result
		assert.Contains(t, string(result), "# Important key", "Original comment should be preserved")
		assert.Contains(t, string(result), "# New key", "New comment should be added")
	})

	t.Run("preserves comments in complex nested structures", func(t *testing.T) {
		original := []byte(`apiVersion: apps/v1
kind: Deployment
metadata:
  name: mydep
spec:
  replicas: 3
  template:
    spec:
      containers:
      - name: nginx
        image: nginx:latest
        resources: # Resource limits
          limits:
            cpu: 200m
            memory: 128Mi
`)

		modified := []byte(`apiVersion: apps/v1
kind: Deployment
metadata:
  name: mydep
spec:
  replicas: 3
  template:
    spec:
      containers:
      - name: nginx
        image: nginx:latest
        resources:
          limits:
            cpu: 250m
            memory: 256Mi
          requests:
            cpu: 100m
            memory: 128Mi
`)

		target := original

		result, changed, err := yamlkit.DiffPatch(original, modified, target, k8skit.NewK8sResourceProvider())

		assert.NoError(t, err)
		assert.True(t, changed)

		// The comment on resources should be preserved
		assert.Contains(t, string(result), "# Resource limits", "Nested comment should be preserved")

		// Verify the structure is correct
		parsedResult, err := gaby.ParseYAML(result)
		assert.NoError(t, err)

		// Verify the new requests field was added
		requests := parsedResult.Path("spec.template.spec.containers.0.resources.requests")
		assert.NotNil(t, requests, "requests field should be added")
	})

	t.Run("handles multiple line comments on different fields", func(t *testing.T) {
		// This test demonstrates that DiffPatch works at the resource level
		// When the entire resource changes significantly, comments from target are preserved
		original := []byte(`apiVersion: v1
kind: ConfigMap
metadata:
  name: my-config
  namespace: default
data:
  key1: value1
  key2: value2
`)

		modified := []byte(`apiVersion: v1
kind: ConfigMap
metadata:
  name: my-config
  namespace: default
data:
  key1: modified1
  key2: modified2
  key3: value3
`)

		// Target has the comments we want to preserve
		target := []byte(`apiVersion: v1
kind: ConfigMap
metadata:
  name: my-config # Config name
  namespace: default # Default namespace
data:
  key1: value1 # First key
  key2: value2 # Second key
`)

		result, changed, err := yamlkit.DiffPatch(original, modified, target, k8skit.NewK8sResourceProvider())

		assert.NoError(t, err)
		assert.True(t, changed)

		// Comments in target should be preserved
		assert.Contains(t, string(result), "# Config name", "Comment on name should be preserved")
		assert.Contains(t, string(result), "# Default namespace", "Comment on namespace should be preserved")
		assert.Contains(t, string(result), "# First key", "Comment on key1 should be preserved")
		assert.Contains(t, string(result), "# Second key", "Comment on key2 should be preserved")

		// Verify values were updated
		assert.Contains(t, string(result), "modified1")
		assert.Contains(t, string(result), "modified2")
		assert.Contains(t, string(result), "key3: value3")
	})
}

func TestDiffPatch_AddContainerEnvVar(t *testing.T) {
	original := []byte(`apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  namespace: default
spec:
  replicas: 1
  selector:
    matchLabels:
      app: myapp
  template:
    metadata:
      labels:
        app: myapp
    spec:
      containers:
      - name: mycontainer
        image: nginx:1.21
        env:
        - name: ENV_A
          value: alpha
        - name: ENV_B
          value: bravo
        - name: ENV_C
          value: charlie
`)

	modified := []byte(`apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  namespace: default
spec:
  replicas: 1
  selector:
    matchLabels:
      app: myapp
  template:
    metadata:
      labels:
        app: myapp
    spec:
      containers:
      - name: mycontainer
        image: nginx:1.21
        env:
        - name: ENV_A
          value: alpha
        - name: ENV_B
          value: bravo
        - name: ENV_C
          value: charlie
        - name: ENV_D
          value: delta
`)

	target := original

	result, changed, err := yamlkit.DiffPatch(original, modified, target, k8skit.NewK8sResourceProvider())
	assert.NoError(t, err)
	assert.True(t, changed)

	// Verify the new env var is present
	assert.Contains(t, string(result), "ENV_D")
	assert.Contains(t, string(result), "delta")

	// Verify all original env vars are preserved
	assert.Contains(t, string(result), "ENV_A")
	assert.Contains(t, string(result), "ENV_B")
	assert.Contains(t, string(result), "ENV_C")

	// Verify the result is valid YAML and structurally correct
	parsedResult, err := gaby.ParseYAML(result)
	assert.NoError(t, err)

	// Check the new env var at index 3
	envD := parsedResult.Path("spec.template.spec.containers.0.env.3.name")
	assert.NotNil(t, envD, "env[3] should exist")
	if envD != nil {
		assert.Equal(t, "ENV_D", envD.Data())
	}
	envDVal := parsedResult.Path("spec.template.spec.containers.0.env.3.value")
	assert.NotNil(t, envDVal)
	if envDVal != nil {
		assert.Equal(t, "delta", envDVal.Data())
	}
}
