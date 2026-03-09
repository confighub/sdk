// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/confighub/sdk/configkit/k8skit"
	"github.com/confighub/sdk/function/api"
	"github.com/confighub/sdk/third_party/gaby"
)

const testDeployment = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: test-deployment
  namespace: default
  labels:
    app: test
spec:
  replicas: 3
  selector:
    matchLabels:
      app: test
  template:
    metadata:
      labels:
        app: test
    spec:
      containers:
      - name: test-container
        image: nginx:1.21
        ports:
        - containerPort: 80
`

const testDeploymentNoLabels = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: no-labels-deployment
  namespace: default
spec:
  replicas: 1
  selector:
    matchLabels:
      app: test
  template:
    metadata:
      labels:
        app: test
    spec:
      containers:
      - name: test-container
        image: nginx:latest
`

const requireLabelsPolicy = `apiVersion: kyverno.io/v1
kind: ClusterPolicy
metadata:
  name: require-labels
spec:
  validationFailureAction: Enforce
  rules:
  - name: check-for-labels
    match:
      any:
      - resources:
          kinds:
          - Deployment
    validate:
      message: "The label 'app' is required."
      pattern:
        metadata:
          labels:
            app: "?*"
`

const disallowLatestTagPolicy = `apiVersion: kyverno.io/v1
kind: ClusterPolicy
metadata:
  name: disallow-latest-tag
spec:
  validationFailureAction: Enforce
  rules:
  - name: validate-image-tag
    match:
      any:
      - resources:
          kinds:
          - Deployment
    validate:
      message: "Using 'latest' tag is not allowed."
      pattern:
        spec:
          template:
            spec:
              containers:
              - image: "!*:latest"
`

const multiDocResources = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: deploy-one
  namespace: default
  labels:
    app: one
spec:
  replicas: 1
  selector:
    matchLabels:
      app: one
  template:
    metadata:
      labels:
        app: one
    spec:
      containers:
      - name: c1
        image: nginx:1.21
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: deploy-two
  namespace: default
  labels:
    app: two
spec:
  replicas: 1
  selector:
    matchLabels:
      app: two
  template:
    metadata:
      labels:
        app: two
    spec:
      containers:
      - name: c2
        image: nginx:1.22
`

func requireKyvernoCLI(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath(kyvernoBinary); err != nil {
		t.Skipf("kyverno CLI not found in PATH (set kyvernoBinary or install kyverno): %v", err)
	}
}

func TestVetKyverno_PassingValidation(t *testing.T) {
	requireKyvernoCLI(t)

	parsedData, err := gaby.ParseAll([]byte(testDeployment))
	require.NoError(t, err)

	args := []api.FunctionArgument{
		{Value: requireLabelsPolicy},
	}

	rp := k8skit.NewK8sResourceProvider()
	result, output, err := vetKyverno(rp, parsedData, args)
	require.NoError(t, err)
	require.NotNil(t, result)

	vr, ok := output.(api.ValidationResult)
	require.True(t, ok, "expected ValidationResult, got %T", output)
	assert.True(t, vr.Passed, "expected validation to pass")
}

func TestVetKyverno_FailingValidation(t *testing.T) {
	requireKyvernoCLI(t)

	parsedData, err := gaby.ParseAll([]byte(testDeploymentNoLabels))
	require.NoError(t, err)

	args := []api.FunctionArgument{
		{Value: requireLabelsPolicy},
	}

	rp := k8skit.NewK8sResourceProvider()
	result, output, err := vetKyverno(rp, parsedData, args)
	require.NoError(t, err)
	require.NotNil(t, result)

	vr, ok := output.(api.ValidationResult)
	require.True(t, ok, "expected ValidationResult, got %T", output)
	assert.False(t, vr.Passed, "expected validation to fail")
	assert.NotEmpty(t, vr.Details, "expected failure details")
	assert.Contains(t, vr.Details[0], "require-labels")
}

func TestVetKyverno_LatestTagPolicy(t *testing.T) {
	requireKyvernoCLI(t)

	parsedData, err := gaby.ParseAll([]byte(testDeploymentNoLabels))
	require.NoError(t, err)

	args := []api.FunctionArgument{
		{Value: disallowLatestTagPolicy},
	}

	rp := k8skit.NewK8sResourceProvider()
	_, output, err := vetKyverno(rp, parsedData, args)
	require.NoError(t, err)

	vr, ok := output.(api.ValidationResult)
	require.True(t, ok)
	assert.False(t, vr.Passed, "expected validation to fail for latest tag")
	assert.NotEmpty(t, vr.Details)
	assert.Contains(t, vr.Details[0], "disallow-latest-tag")
}

func TestVetKyverno_LatestTagPolicy_Passing(t *testing.T) {
	requireKyvernoCLI(t)

	parsedData, err := gaby.ParseAll([]byte(testDeployment))
	require.NoError(t, err)

	args := []api.FunctionArgument{
		{Value: disallowLatestTagPolicy},
	}

	rp := k8skit.NewK8sResourceProvider()
	_, output, err := vetKyverno(rp, parsedData, args)
	require.NoError(t, err)

	vr, ok := output.(api.ValidationResult)
	require.True(t, ok)
	assert.True(t, vr.Passed, "expected validation to pass for tagged image")
}

func TestVetKyverno_MultipleResources(t *testing.T) {
	requireKyvernoCLI(t)

	parsedData, err := gaby.ParseAll([]byte(multiDocResources))
	require.NoError(t, err)
	require.Len(t, parsedData, 2, "expected 2 documents")

	args := []api.FunctionArgument{
		{Value: requireLabelsPolicy},
	}

	rp := k8skit.NewK8sResourceProvider()
	_, output, err := vetKyverno(rp, parsedData, args)
	require.NoError(t, err)

	vr, ok := output.(api.ValidationResult)
	require.True(t, ok)
	assert.True(t, vr.Passed, "expected both resources to pass")
}

func TestVetKyverno_MultiplePolicies(t *testing.T) {
	requireKyvernoCLI(t)

	multiPolicy := requireLabelsPolicy + "\n---\n" + disallowLatestTagPolicy

	parsedData, err := gaby.ParseAll([]byte(testDeployment))
	require.NoError(t, err)

	args := []api.FunctionArgument{
		{Value: multiPolicy},
	}

	rp := k8skit.NewK8sResourceProvider()
	_, output, err := vetKyverno(rp, parsedData, args)
	require.NoError(t, err)

	vr, ok := output.(api.ValidationResult)
	require.True(t, ok)
	assert.True(t, vr.Passed, "expected all policies to pass")
}

func TestVetKyverno_FailedAttributes(t *testing.T) {
	requireKyvernoCLI(t)

	parsedData, err := gaby.ParseAll([]byte(testDeploymentNoLabels))
	require.NoError(t, err)

	args := []api.FunctionArgument{
		{Value: requireLabelsPolicy},
	}

	rp := k8skit.NewK8sResourceProvider()
	_, output, err := vetKyverno(rp, parsedData, args)
	require.NoError(t, err)

	vr, ok := output.(api.ValidationResult)
	require.True(t, ok)
	assert.False(t, vr.Passed)
	assert.NotEmpty(t, vr.FailedAttributes, "expected failed attributes")

	attr := vr.FailedAttributes[0]
	assert.NotNil(t, attr.Details, "expected attribute details")
	assert.Contains(t, attr.Details.Description, "require-labels")
}

func TestVetKyverno_PathExtraction(t *testing.T) {
	requireKyvernoCLI(t)

	parsedData, err := gaby.ParseAll([]byte(testDeploymentNoLabels))
	require.NoError(t, err)

	args := []api.FunctionArgument{
		{Value: requireLabelsPolicy},
	}

	rp := k8skit.NewK8sResourceProvider()
	_, output, err := vetKyverno(rp, parsedData, args)
	require.NoError(t, err)

	vr, ok := output.(api.ValidationResult)
	require.True(t, ok)
	assert.False(t, vr.Passed)
	require.NotEmpty(t, vr.FailedAttributes)

	// The require-labels policy should report a path like "metadata.labels"
	path := string(vr.FailedAttributes[0].Path)
	assert.NotEmpty(t, path, "expected a field path from kyverno output")
}

func TestVetKyverno_MissingBinary(t *testing.T) {
	old := kyvernoBinary
	kyvernoBinary = "nonexistent-kyverno-binary-12345"
	defer func() { kyvernoBinary = old }()

	parsedData, err := gaby.ParseAll([]byte(testDeployment))
	require.NoError(t, err)

	args := []api.FunctionArgument{
		{Value: requireLabelsPolicy},
	}

	rp := k8skit.NewK8sResourceProvider()
	_, _, err = vetKyverno(rp, parsedData, args)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to execute kyverno CLI")
}

// Unit tests for parsing (don't need the kyverno binary).

func TestParseKyvernoOutput(t *testing.T) {
	output := `Applying 1 policy rule(s) to 1 resource(s)...
policy require-labels -> resource default/Deployment/test failed:
1 - check-for-labels validation error: The label 'app' is required. rule check-for-labels failed at path /metadata/labels/


pass: 0, fail: 1, warn: 0, error: 0, skip: 0
`
	failures := parseKyvernoOutput(output)
	require.Len(t, failures, 1)
	assert.Equal(t, "require-labels", failures[0].policyName)
	assert.Equal(t, "check-for-labels", failures[0].ruleName)
	assert.Equal(t, "/metadata/labels", failures[0].path)
	assert.Equal(t, "default/Deployment/test", failures[0].resourceKey)
	assert.Contains(t, failures[0].message, "The label 'app' is required")
}

func TestParseKyvernoOutput_MultipleFailures(t *testing.T) {
	output := `Applying 2 policy rule(s) to 2 resource(s)...
policy require-labels -> resource default/Deployment/deploy-one failed:
1 - check-for-labels validation error: The label 'app' is required. rule check-for-labels failed at path /metadata/labels/

policy require-labels -> resource default/Deployment/deploy-two failed:
1 - check-for-labels validation error: The label 'app' is required. rule check-for-labels failed at path /metadata/labels/


pass: 0, fail: 2, warn: 0, error: 0, skip: 0
`
	failures := parseKyvernoOutput(output)
	require.Len(t, failures, 2)
	assert.Equal(t, "default/Deployment/deploy-one", failures[0].resourceKey)
	assert.Equal(t, "default/Deployment/deploy-two", failures[1].resourceKey)
}

func TestParseKyvernoOutput_NoPath(t *testing.T) {
	output := `policy my-policy -> resource default/Pod/test failed:
1 - my-rule validation error: some message without a path


pass: 0, fail: 1, warn: 0, error: 0, skip: 0
`
	failures := parseKyvernoOutput(output)
	require.Len(t, failures, 1)
	assert.Equal(t, "", failures[0].path)
}
