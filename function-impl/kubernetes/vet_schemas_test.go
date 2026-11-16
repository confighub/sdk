// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package kubernetes

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/third_party/gaby"
)

const vetSchemasValidDeployment = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: nginx
  namespace: default
spec:
  replicas: 3
  selector:
    matchLabels:
      app: nginx
  template:
    metadata:
      labels:
        app: nginx
    spec:
      containers:
      - name: nginx
        image: nginx:1.21
`

const vetSchemasInvalidField = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: nginx
  namespace: default
spec:
  replicas: 3
  bogusField: true
  selector:
    matchLabels:
      app: nginx
  template:
    metadata:
      labels:
        app: nginx
    spec:
      containers:
      - name: nginx
        image: nginx:1.21
`

const vetSchemasInvalidNestedField = `apiVersion: v1
kind: Service
metadata:
  name: my-service
spec:
  selector:
    app: nginx
  ports:
  - port: 80
    targetPort: 8080
    notAField: bad
  type: ClusterIP
`

const vetSchemasMultipleInvalid = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: nginx
spec:
  replicas: 3
  bogusField: true
  selector:
    matchLabels:
      app: nginx
  template:
    metadata:
      labels:
        app: nginx
    spec:
      containers:
      - name: nginx
        image: nginx:1.21
---
apiVersion: v1
kind: Service
metadata:
  name: my-service
spec:
  selector:
    app: nginx
  ports:
  - port: 80
    targetPort: 8080
    fakeField: bad
  type: ClusterIP
`

// networking.k8s.io/v1 Ingress with old beta-style fields that are invalid in v1
const vetSchemasDeprecatedAPIFields = `apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: test-ingress
spec:
  rules:
  - host: example.com
    http:
      paths:
      - path: /
        pathType: Prefix
        backend:
          serviceName: my-service
          servicePort: 80
`

func TestK8sFnVetSchemas_ValidResource(t *testing.T) {
	docs, err := gaby.ParseAll([]byte(vetSchemasValidDeployment))
	require.NoError(t, err)

	_, output, err := k8sFnVetSchemas(testResourceProvider, nil, nil, docs, nil)
	require.NoError(t, err)

	vr, ok := output.(api.ValidationResult)
	require.True(t, ok)
	assert.True(t, vr.Passed)
	assert.Empty(t, vr.FailedAttributes)
}

func TestK8sFnVetSchemas_InvalidField(t *testing.T) {
	docs, err := gaby.ParseAll([]byte(vetSchemasInvalidField))
	require.NoError(t, err)

	_, output, err := k8sFnVetSchemas(testResourceProvider, nil, nil, docs, nil)
	require.NoError(t, err)

	vr, ok := output.(api.ValidationResult)
	require.True(t, ok)
	assert.False(t, vr.Passed)
	require.NotEmpty(t, vr.FailedAttributes)

	// The invalid field "bogusField" should appear in a failed path
	foundBogus := false
	for _, attr := range vr.FailedAttributes {
		if string(attr.Path) == "spec.bogusField" {
			foundBogus = true
			break
		}
	}
	assert.True(t, foundBogus, "expected to find spec.bogusField in failed paths, got: %v", vr.FailedAttributes)
}

func TestK8sFnVetSchemas_InvalidNestedField(t *testing.T) {
	docs, err := gaby.ParseAll([]byte(vetSchemasInvalidNestedField))
	require.NoError(t, err)

	_, output, err := k8sFnVetSchemas(testResourceProvider, nil, nil, docs, nil)
	require.NoError(t, err)

	vr, ok := output.(api.ValidationResult)
	require.True(t, ok)
	assert.False(t, vr.Passed)
	require.NotEmpty(t, vr.FailedAttributes)

	foundNotAField := false
	for _, attr := range vr.FailedAttributes {
		if string(attr.Path) == "spec.ports.0.notAField" {
			foundNotAField = true
			break
		}
	}
	assert.True(t, foundNotAField, "expected to find spec.ports.0.notAField in failed paths, got: %v", vr.FailedAttributes)
}

func TestK8sFnVetSchemas_MultipleInvalidResources(t *testing.T) {
	docs, err := gaby.ParseAll([]byte(vetSchemasMultipleInvalid))
	require.NoError(t, err)

	_, output, err := k8sFnVetSchemas(testResourceProvider, nil, nil, docs, nil)
	require.NoError(t, err)

	vr, ok := output.(api.ValidationResult)
	require.True(t, ok)
	assert.False(t, vr.Passed)

	// Should have failed paths from both resources
	paths := make(map[string]bool)
	for _, attr := range vr.FailedAttributes {
		paths[string(attr.Path)] = true
	}
	assert.True(t, paths["spec.bogusField"], "expected spec.bogusField in failed paths")
	assert.True(t, paths["spec.ports.0.fakeField"], "expected spec.ports.0.fakeField in failed paths")
}

func TestK8sFnVetSchemas_DeprecatedAPIFields(t *testing.T) {
	docs, err := gaby.ParseAll([]byte(vetSchemasDeprecatedAPIFields))
	require.NoError(t, err)

	// Use a Kubernetes version where the old beta-style Ingress backend fields are invalid.
	// networking.k8s.io/v1 Ingress requires backend.service instead of backend.serviceName/servicePort.
	args := stringArgsToFunctionArgs([]string{"1.30.0"})

	_, output, err := k8sFnVetSchemas(testResourceProvider, nil, nil, docs, args)
	require.NoError(t, err)

	vr, ok := output.(api.ValidationResult)
	require.True(t, ok)
	assert.False(t, vr.Passed, "expected validation to fail for deprecated API fields")
	require.NotEmpty(t, vr.FailedAttributes, "expected failed attributes for invalid backend fields")

	// The old beta-style fields should be flagged under the backend path
	paths := make(map[string]bool)
	for _, attr := range vr.FailedAttributes {
		paths[string(attr.Path)] = true
	}
	// kubeconform may report "serviceName" and "servicePort" as separate additionalProperties errors
	// or combined in one. Check that at least one backend field path is reported.
	hasBackendError := paths["spec.rules.0.http.paths.0.backend.serviceName"] ||
		paths["spec.rules.0.http.paths.0.backend.servicePort"]
	assert.True(t, hasBackendError,
		"expected at least one deprecated backend field in failed paths, got: %v", paths)
}

func TestK8sFnVetSchemas_KubernetesVersion(t *testing.T) {
	docs, err := gaby.ParseAll([]byte(vetSchemasValidDeployment))
	require.NoError(t, err)

	args := stringArgsToFunctionArgs([]string{"1.26.12"})

	_, output, err := k8sFnVetSchemas(testResourceProvider, nil, nil, docs, args)
	require.NoError(t, err)

	vr, ok := output.(api.ValidationResult)
	require.True(t, ok)
	assert.True(t, vr.Passed)
}

func TestTargetSchemaVersion(t *testing.T) {
	facts := func(version string) *api.FunctionContext {
		return &api.FunctionContext{TargetFacts: map[string]string{kubernetesVersionFact: version}}
	}
	for version, want := range map[string]string{
		"1.31.2":            "1.31.0",
		"v1.33.5":           "1.33.0",
		"1.32":              "1.32.0",
		"v1.30.4-eks-a1b2c": "1.30.0",
		"":                  "",
		"latest":            "",
		"1.31x":             "",
	} {
		assert.Equal(t, want, targetSchemaVersion(facts(version)), version)
	}
	assert.Equal(t, "", targetSchemaVersion(nil))
	assert.Equal(t, "", targetSchemaVersion(&api.FunctionContext{}))
}

const vetSchemasMissingAndWrongType = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: nginx
  namespace: default
  annotations:
    example.com/owner: 3
spec:
  replicas: three
  template:
    metadata:
      labels:
        app: nginx
    spec:
      containers:
      - name: nginx
        image: nginx:1.21
`

// Each finding is located at the field it is about, carries the error as an Issue identified by its
// kind, and the value found when it is a scalar: a missing property at the property, not at the
// object missing it, and a key with a dot or slash escaped as the Unit's paths escape it.
func TestK8sFnVetSchemas_FindingsNameTheFieldAndTheKind(t *testing.T) {
	docs, err := gaby.ParseAll([]byte(vetSchemasMissingAndWrongType))
	require.NoError(t, err)

	_, output, err := k8sFnVetSchemas(testResourceProvider, nil, nil, docs, nil)
	require.NoError(t, err)
	vr, ok := output.(api.ValidationResult)
	require.True(t, ok)
	assert.False(t, vr.Passed)

	byPath := map[string]api.AttributeValue{}
	for _, attr := range vr.FailedAttributes {
		require.Len(t, attr.Issues, 1, "each finding carries its error: %v", attr)
		assert.Equal(t, attr.Issues[0].Message != "", true)
		byPath[string(attr.Path)] = attr
	}
	missing, ok := byPath["spec.selector"]
	require.True(t, ok, "expected the missing selector at spec.selector, got: %v", vr.FailedAttributes)
	assert.Equal(t, "required", missing.Issues[0].Identifier)

	wrongType, ok := byPath["spec.replicas"]
	require.True(t, ok, "expected the replicas of the wrong type at spec.replicas, got: %v", vr.FailedAttributes)
	assert.Equal(t, "type", wrongType.Issues[0].Identifier)
	assert.Equal(t, "three", wrongType.Value)

	annotation, ok := byPath["metadata.annotations.example~1com/owner"]
	require.True(t, ok, "expected the annotation at its escaped path, got: %v", vr.FailedAttributes)
	assert.Equal(t, "type", annotation.Issues[0].Identifier)
}

// A property the schema does not have is reported at the property, identified as such.
func TestK8sFnVetSchemas_UnknownPropertyIsIdentified(t *testing.T) {
	docs, err := gaby.ParseAll([]byte(vetSchemasInvalidField))
	require.NoError(t, err)
	_, output, err := k8sFnVetSchemas(testResourceProvider, nil, nil, docs, nil)
	require.NoError(t, err)
	vr := output.(api.ValidationResult)
	for _, attr := range vr.FailedAttributes {
		if string(attr.Path) == "spec.bogusField" {
			require.Len(t, attr.Issues, 1)
			assert.Equal(t, "additionalProperties", attr.Issues[0].Identifier)
			return
		}
	}
	t.Fatalf("expected spec.bogusField, got: %v", vr.FailedAttributes)
}

const vetSchemasIntOrString = `apiVersion: v1
kind: Service
metadata:
  name: web
  namespace: default
spec:
  ports:
  - port: 80
    targetPort: true
`

// An IntOrString field the value fits neither alternative of is one finding, at the field, naming
// both alternatives: each alternative failing is not a problem of its own.
func TestK8sFnVetSchemas_OneOfIsOneFinding(t *testing.T) {
	docs, err := gaby.ParseAll([]byte(vetSchemasIntOrString))
	require.NoError(t, err)
	_, output, err := k8sFnVetSchemas(testResourceProvider, nil, nil, docs, nil)
	require.NoError(t, err)
	vr := output.(api.ValidationResult)
	assert.False(t, vr.Passed)
	require.Len(t, vr.FailedAttributes, 1, "expected one finding, got: %v", vr.FailedAttributes)
	finding := vr.FailedAttributes[0]
	assert.Equal(t, "spec.ports.0.targetPort", string(finding.Path))
	assert.Equal(t, true, finding.Value)
	require.Len(t, finding.Issues, 1)
	assert.Equal(t, "oneOf", finding.Issues[0].Identifier)
	assert.Contains(t, finding.Issues[0].Message, "expected string")
	assert.Contains(t, finding.Issues[0].Message, "expected integer")
}
