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

	_, output, err := k8sFnVetSchemas(testResourceProvider, nil, docs, nil)
	require.NoError(t, err)

	vr, ok := output.(api.ValidationResult)
	require.True(t, ok)
	assert.True(t, vr.Passed)
	assert.Empty(t, vr.FailedAttributes)
}

func TestK8sFnVetSchemas_InvalidField(t *testing.T) {
	docs, err := gaby.ParseAll([]byte(vetSchemasInvalidField))
	require.NoError(t, err)

	_, output, err := k8sFnVetSchemas(testResourceProvider, nil, docs, nil)
	require.NoError(t, err)

	vr, ok := output.(api.ValidationResult)
	require.True(t, ok)
	assert.False(t, vr.Passed)
	require.NotEmpty(t, vr.FailedAttributes)

	// The invalid field "bogusField" should appear in a failed path
	foundBogus := false
	for _, attr := range vr.FailedAttributes {
		if string(attr.Path) == ".spec.bogusField" {
			foundBogus = true
			break
		}
	}
	assert.True(t, foundBogus, "expected to find .spec.bogusField in failed paths, got: %v", vr.FailedAttributes)
}

func TestK8sFnVetSchemas_InvalidNestedField(t *testing.T) {
	docs, err := gaby.ParseAll([]byte(vetSchemasInvalidNestedField))
	require.NoError(t, err)

	_, output, err := k8sFnVetSchemas(testResourceProvider, nil, docs, nil)
	require.NoError(t, err)

	vr, ok := output.(api.ValidationResult)
	require.True(t, ok)
	assert.False(t, vr.Passed)
	require.NotEmpty(t, vr.FailedAttributes)

	foundNotAField := false
	for _, attr := range vr.FailedAttributes {
		if string(attr.Path) == ".spec.ports.0.notAField" {
			foundNotAField = true
			break
		}
	}
	assert.True(t, foundNotAField, "expected to find .spec.ports.0.notAField in failed paths, got: %v", vr.FailedAttributes)
}

func TestK8sFnVetSchemas_MultipleInvalidResources(t *testing.T) {
	docs, err := gaby.ParseAll([]byte(vetSchemasMultipleInvalid))
	require.NoError(t, err)

	_, output, err := k8sFnVetSchemas(testResourceProvider, nil, docs, nil)
	require.NoError(t, err)

	vr, ok := output.(api.ValidationResult)
	require.True(t, ok)
	assert.False(t, vr.Passed)

	// Should have failed paths from both resources
	paths := make(map[string]bool)
	for _, attr := range vr.FailedAttributes {
		paths[string(attr.Path)] = true
	}
	assert.True(t, paths[".spec.bogusField"], "expected .spec.bogusField in failed paths")
	assert.True(t, paths[".spec.ports.0.fakeField"], "expected .spec.ports.0.fakeField in failed paths")
}

func TestK8sFnVetSchemas_DeprecatedAPIFields(t *testing.T) {
	docs, err := gaby.ParseAll([]byte(vetSchemasDeprecatedAPIFields))
	require.NoError(t, err)

	// Use a Kubernetes version where the old beta-style Ingress backend fields are invalid.
	// networking.k8s.io/v1 Ingress requires backend.service instead of backend.serviceName/servicePort.
	args := stringArgsToFunctionArgs([]string{"1.30.0"})

	_, output, err := k8sFnVetSchemas(testResourceProvider, nil, docs, args)
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
	hasBackendError := paths[".spec.rules.0.http.paths.0.backend.serviceName"] ||
		paths[".spec.rules.0.http.paths.0.backend.servicePort"]
	assert.True(t, hasBackendError,
		"expected at least one deprecated backend field in failed paths, got: %v", paths)
}

func TestK8sFnVetSchemas_KubernetesVersion(t *testing.T) {
	docs, err := gaby.ParseAll([]byte(vetSchemasValidDeployment))
	require.NoError(t, err)

	args := stringArgsToFunctionArgs([]string{"1.26.12"})

	_, output, err := k8sFnVetSchemas(testResourceProvider, nil, docs, args)
	require.NoError(t, err)

	vr, ok := output.(api.ValidationResult)
	require.True(t, ok)
	assert.True(t, vr.Passed)
}
