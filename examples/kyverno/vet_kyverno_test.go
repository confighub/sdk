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

const requireLabelsPolicy = `apiVersion: policies.kyverno.io/v1
kind: ValidatingPolicy
metadata:
  name: require-labels
spec:
  validationActions: [Deny]
  matchConstraints:
    resourceRules:
      - apiGroups: ['apps']
        apiVersions: [v1]
        operations: [CREATE, UPDATE]
        resources: [deployments]
  validations:
    - expression: >
        has(object.metadata.labels) &&
        'app' in object.metadata.labels &&
        object.metadata.labels['app'] != ''
      message: "The label 'app' is required."
`

const disallowLatestTagPolicy = `apiVersion: policies.kyverno.io/v1
kind: ValidatingPolicy
metadata:
  name: disallow-latest-tag
spec:
  validationActions: [Deny]
  matchConstraints:
    resourceRules:
      - apiGroups: ['apps']
        apiVersions: [v1]
        operations: [CREATE, UPDATE]
        resources: [deployments]
  validations:
    - expression: >
        object.spec.template.spec.containers.all(c,
          !c.image.endsWith(':latest')
        )
      message: "Using 'latest' tag is not allowed."
`

// Kubernetes native ValidatingAdmissionPolicy (admissionregistration.k8s.io/v1).
const requireLabelsVAP = `apiVersion: admissionregistration.k8s.io/v1
kind: ValidatingAdmissionPolicy
metadata:
  name: require-labels
spec:
  matchConstraints:
    resourceRules:
      - apiGroups: ['apps']
        apiVersions: [v1]
        operations: [CREATE, UPDATE]
        resources: [deployments]
  validations:
    - expression: >
        has(object.metadata.labels) &&
        'app' in object.metadata.labels &&
        object.metadata.labels['app'] != ''
      message: "The label 'app' is required."
`

const disallowLatestTagVAP = `apiVersion: admissionregistration.k8s.io/v1
kind: ValidatingAdmissionPolicy
metadata:
  name: disallow-latest-tag
spec:
  matchConstraints:
    resourceRules:
      - apiGroups: ['apps']
        apiVersions: [v1]
        operations: [CREATE, UPDATE]
        resources: [deployments]
  validations:
    - expression: >
        object.spec.template.spec.containers.all(c,
          !c.image.endsWith(':latest')
        )
      message: "Using 'latest' tag is not allowed."
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
	assert.NotEmpty(t, attr.Issues, "expected attribute issues")
	assert.Contains(t, attr.Issues[0].Identifier, "require-labels")
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

// Unit tests for JSON parsing (don't need the kyverno binary).

func TestParsePolicyReport_Failures(t *testing.T) {
	jsonOutput := `{"kind":"ClusterReport","apiVersion":"openreports.io/v1alpha1","metadata":{"name":"merged"},"source":"","summary":{"pass":0,"fail":2,"warn":0,"error":0,"skip":0},"results":[{"source":"KyvernoValidatingPolicy","policy":"require-labels","timestamp":{"seconds":1773775408,"nanos":0},"result":"fail","scored":true,"resources":[{"kind":"Deployment","namespace":"default","name":"no-labels-deployment","apiVersion":"apps/v1"}],"message":"The label 'app' is required.","properties":{"process":"background scan"}},{"source":"KyvernoValidatingPolicy","policy":"disallow-latest-tag","timestamp":{"seconds":1773775408,"nanos":0},"result":"fail","scored":true,"resources":[{"kind":"Deployment","namespace":"default","name":"no-labels-deployment","apiVersion":"apps/v1"}],"message":"Using 'latest' tag is not allowed.","properties":{"process":"background scan"}}]}`

	report, err := parsePolicyReport([]byte(jsonOutput))
	require.NoError(t, err)
	assert.Equal(t, 0, report.Summary.Pass)
	assert.Equal(t, 2, report.Summary.Fail)
	require.Len(t, report.Results, 2)

	assert.Equal(t, "require-labels", report.Results[0].Policy)
	assert.Equal(t, "fail", report.Results[0].Result)
	assert.Equal(t, "The label 'app' is required.", report.Results[0].Message)
	require.Len(t, report.Results[0].Resources, 1)
	assert.Equal(t, "Deployment", report.Results[0].Resources[0].Kind)
	assert.Equal(t, "default", report.Results[0].Resources[0].Namespace)
	assert.Equal(t, "no-labels-deployment", report.Results[0].Resources[0].Name)

	assert.Equal(t, "disallow-latest-tag", report.Results[1].Policy)
	assert.Equal(t, "Using 'latest' tag is not allowed.", report.Results[1].Message)
}

func TestParsePolicyReport_Passing(t *testing.T) {
	jsonOutput := `{"kind":"ClusterReport","apiVersion":"openreports.io/v1alpha1","metadata":{"name":"merged"},"source":"","summary":{"pass":1,"fail":0,"warn":0,"error":0,"skip":0},"results":[{"source":"KyvernoValidatingPolicy","policy":"require-labels","result":"pass","resources":[{"kind":"Deployment","namespace":"default","name":"test-deployment","apiVersion":"apps/v1"}],"message":"success"}]}`

	report, err := parsePolicyReport([]byte(jsonOutput))
	require.NoError(t, err)
	assert.Equal(t, 1, report.Summary.Pass)
	assert.Equal(t, 0, report.Summary.Fail)
}

func TestParsePolicyReport_ClusterPolicy(t *testing.T) {
	jsonOutput := `{"kind":"ClusterReport","apiVersion":"openreports.io/v1alpha1","metadata":{"name":"merged"},"source":"","summary":{"pass":0,"fail":1,"warn":0,"error":0,"skip":0},"results":[{"source":"kyverno","policy":"require-labels","rule":"check-for-labels","result":"fail","resources":[{"kind":"Deployment","namespace":"default","name":"no-labels-deployment","apiVersion":"apps/v1"}],"message":"validation error: The label 'app' is required. rule check-for-labels failed at path /metadata/labels/"}]}`

	report, err := parsePolicyReport([]byte(jsonOutput))
	require.NoError(t, err)
	assert.Equal(t, 1, report.Summary.Fail)
	require.Len(t, report.Results, 1)
	assert.Equal(t, "require-labels", report.Results[0].Policy)
	assert.Equal(t, "check-for-labels", report.Results[0].Rule)
}

func TestParsePolicyReport_NoJSON(t *testing.T) {
	_, err := parsePolicyReport([]byte("not json at all"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no JSON found")
}

// Tests for Kubernetes native ValidatingAdmissionPolicy.

func TestVetKyverno_ValidatingAdmissionPolicy_Passing(t *testing.T) {
	requireKyvernoCLI(t)

	parsedData, err := gaby.ParseAll([]byte(testDeployment))
	require.NoError(t, err)

	args := []api.FunctionArgument{
		{Value: requireLabelsVAP},
	}

	rp := k8skit.NewK8sResourceProvider()
	_, output, err := vetKyverno(rp, parsedData, args)
	require.NoError(t, err)

	vr, ok := output.(api.ValidationResult)
	require.True(t, ok, "expected ValidationResult, got %T", output)
	assert.True(t, vr.Passed, "expected validation to pass")
}

func TestVetKyverno_ValidatingAdmissionPolicy_Failing(t *testing.T) {
	requireKyvernoCLI(t)

	parsedData, err := gaby.ParseAll([]byte(testDeploymentNoLabels))
	require.NoError(t, err)

	args := []api.FunctionArgument{
		{Value: requireLabelsVAP},
	}

	rp := k8skit.NewK8sResourceProvider()
	_, output, err := vetKyverno(rp, parsedData, args)
	require.NoError(t, err)

	vr, ok := output.(api.ValidationResult)
	require.True(t, ok, "expected ValidationResult, got %T", output)
	assert.False(t, vr.Passed, "expected validation to fail")
	assert.NotEmpty(t, vr.Details, "expected failure details")
	assert.Contains(t, vr.Details[0], "require-labels")
}

func TestVetKyverno_ValidatingAdmissionPolicy_LatestTag(t *testing.T) {
	requireKyvernoCLI(t)

	parsedData, err := gaby.ParseAll([]byte(testDeploymentNoLabels))
	require.NoError(t, err)

	args := []api.FunctionArgument{
		{Value: disallowLatestTagVAP},
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

func TestVetKyverno_MixedPolicies(t *testing.T) {
	requireKyvernoCLI(t)

	// Mix a Kyverno ValidatingPolicy with a K8s ValidatingAdmissionPolicy.
	mixedPolicies := requireLabelsPolicy + "\n---\n" + disallowLatestTagVAP

	parsedData, err := gaby.ParseAll([]byte(testDeployment))
	require.NoError(t, err)

	args := []api.FunctionArgument{
		{Value: mixedPolicies},
	}

	rp := k8skit.NewK8sResourceProvider()
	_, output, err := vetKyverno(rp, parsedData, args)
	require.NoError(t, err)

	vr, ok := output.(api.ValidationResult)
	require.True(t, ok)
	assert.True(t, vr.Passed, "expected all policies to pass")
}

// Tests for path extraction.

func TestExtractPathFromMessage(t *testing.T) {
	tests := []struct {
		msg  string
		want string
	}{
		{
			msg:  "validation error: The label 'app' is required. rule check-for-labels failed at path /metadata/labels/",
			want: "metadata.labels",
		},
		{
			msg:  "rule disallow-latest-tag failed at path /spec/template/spec/containers/0/image/",
			want: "spec.template.spec.containers.0.image",
		},
		{
			msg:  "The label 'app' is required.",
			want: "",
		},
		{
			msg:  "",
			want: "",
		},
	}
	for _, tt := range tests {
		got := extractPathFromMessage(tt.msg)
		assert.Equal(t, tt.want, got, "extractPathFromMessage(%q)", tt.msg)
	}
}

func TestJsonPointerToDotNotation(t *testing.T) {
	assert.Equal(t, "spec.template.spec.containers.0.image", jsonPointerToDotNotation("/spec/template/spec/containers/0/image/"))
	assert.Equal(t, "metadata.labels", jsonPointerToDotNotation("/metadata/labels"))
	assert.Equal(t, "", jsonPointerToDotNotation("/"))
	assert.Equal(t, "", jsonPointerToDotNotation(""))
}

func TestParsePolicyReport_WithPath(t *testing.T) {
	jsonOutput := `{"kind":"ClusterReport","apiVersion":"openreports.io/v1alpha1","metadata":{"name":"merged"},"source":"","summary":{"pass":0,"fail":1,"warn":0,"error":0,"skip":0},"results":[{"source":"kyverno","policy":"disallow-latest-tag","rule":"validate-image-tag","result":"fail","resources":[{"kind":"Deployment","namespace":"default","name":"test-deploy","apiVersion":"apps/v1"}],"message":"validation error: Using latest tag is not allowed. rule validate-image-tag failed at path /spec/template/spec/containers/0/image/"}]}`

	report, err := parsePolicyReport([]byte(jsonOutput))
	require.NoError(t, err)
	assert.Equal(t, 1, report.Summary.Fail)

	// Verify the message contains a path that extractPathFromMessage can parse.
	path := extractPathFromMessage(report.Results[0].Message)
	assert.Equal(t, "spec.template.spec.containers.0.image", path)
}
