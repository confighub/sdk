// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package kubernetes

import (
	"testing"

	"github.com/confighub/sdk/configkit/yamlkit"
	"github.com/confighub/sdk/function/api"
	"github.com/confighub/sdk/third_party/gaby"
	"github.com/stretchr/testify/assert"
)

func TestK8sFnResourceWhereMatch_DeploymentReplicasGt1(t *testing.T) {
	yamlFixture := `
apiVersion: v1
kind: ConfigMap
metadata:
  name: example-configmap
data:
  key1: value1
  key2: value2
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: example-deployment
spec:
  replicas: 3
  selector:
    matchLabels:
      app: example
  template:
    metadata:
      labels:
        app: example
    spec:
      containers:
      - name: example-container
        image: nginx:1.14.2
`
	docs, err := gaby.ParseAll([]byte(yamlFixture))
	assert.NoError(t, err)

	args := stringArgsToFunctionArgs([]string{"apps/v1/Deployment", "spec.replicas > 1"})
	_, result, err := k8sFnResourceWhereMatch(&fakeContext, docs, args, []byte{})
	assert.NoError(t, err)
	assert.Equal(t, api.ValidationResultTrue, result)
}

func TestK8sFnResourceWhereMatch_DeploymentImage(t *testing.T) {
	yamlFixture := `
apiVersion: v1
kind: ConfigMap
metadata:
  name: example-configmap
data:
  key1: value1
  key2: value2
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: example-deployment
  annotations:
    confighub.com/UnitSlug: mydep
spec:
  replicas: 3
  selector:
    matchLabels:
      app: example
  template:
    metadata:
      labels:
        app: example
    spec:
      containers:
      - name: otel-sidecar
        image: otel/opentelemetry-collector:latest-amd64
      - name: main
        image: nginx:1.14.2
`
	docs, err := gaby.ParseAll([]byte(yamlFixture))
	assert.NoError(t, err)

	expressions := []string{
		"spec.template.spec.containers.1.image='nginx:1.14.2'",
		"spec.template.spec.containers.*.image='nginx:1.14.2'",
		"spec.template.spec.containers.*?name:container-name.image='nginx:1.14.2'",
		"spec.template.spec.containers.?name=main.image='nginx:1.14.2'",
		"spec.template.spec.containers.?name:container-name=main.image='nginx:1.14.2'",
		"spec.template.spec.containers.?name=main.image#uri='nginx'",
		"spec.template.spec.containers.?name=main.image#reference=':1.14.2'",
		"spec.template.spec.containers.*.image#reference=':1.14.2'",
	}

	for _, expression := range expressions {
		args := stringArgsToFunctionArgs([]string{"apps/v1/Deployment", expression})
		_, result, err := k8sFnResourceWhereMatch(&fakeContext, docs, args, []byte{})
		assert.NoError(t, err)
		assert.Equal(t, api.ValidationResultTrue, result, "Expression %s expected true", expression)
	}
}

func TestK8sFnResourceWhereMatch_Annotation(t *testing.T) {
	yamlFixture := `
apiVersion: v1
kind: ConfigMap
metadata:
  name: example-configmap
data:
  key1: value1
  key2: value2
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: example-deployment
  annotations:
    confighub.com/key: mydep
spec:
  replicas: 3
  selector:
    matchLabels:
      app: example
  template:
    metadata:
      labels:
        app: example
    spec:
      containers:
      - name: example-container
        image: nginx:1.14.2
`
	docs, err := gaby.ParseAll([]byte(yamlFixture))
	assert.NoError(t, err)

	annotationKey := yamlkit.EscapeDotsInPathSegment("confighub.com/key")

	expressions := []string{
		"metadata.annotations." + annotationKey + "='mydep'",
		"metadata.annotations.@" + annotationKey + ":annotation-key='mydep'",
	}

	for _, expression := range expressions {
		args := stringArgsToFunctionArgs([]string{"apps/v1/Deployment", expression})
		_, result, err := k8sFnResourceWhereMatch(&fakeContext, docs, args, []byte{})
		assert.NoError(t, err)
		assert.Equal(t, api.ValidationResultTrue, result, "Expression %s expected true", expression)
	}
}

func TestK8sFnResourceWhereMatch_ConfigMapBlankWhere(t *testing.T) {
	yamlFixture := `
apiVersion: v1
kind: ConfigMap
metadata:
  name: test-config
`
	docs, err := gaby.ParseAll([]byte(yamlFixture))
	assert.NoError(t, err)

	args := stringArgsToFunctionArgs([]string{"v1/ConfigMap", ""})
	_, result, err := k8sFnResourceWhereMatch(&fakeContext, docs, args, []byte{})
	assert.NoError(t, err)
	assert.Equal(t, api.ValidationResultTrue, result)
}

func TestK8sFnResourceWhereMatch_SplitPathSecurityContext(t *testing.T) {
	yamlFixture := `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: secure-deployment
spec:
  replicas: 1
  selector:
    matchLabels:
      app: secure
  template:
    metadata:
      labels:
        app: secure
    spec:
      containers:
      - name: secure-container
        image: nginx:1.20
        securityContext:
          runAsNonRoot: true
      - name: insecure-container
        image: nginx:1.20
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: insecure-deployment
spec:
  replicas: 1
  selector:
    matchLabels:
      app: insecure
  template:
    metadata:
      labels:
        app: insecure
    spec:
      containers:
      - name: container-without-security
        image: nginx:1.20
`
	docs, err := gaby.ParseAll([]byte(yamlFixture))
	assert.NoError(t, err)

	// Test cases for split path syntax
	testCases := []struct {
		name           string
		expression     string
		expectedResult api.ValidationResult
	}{
		{
			name:           "Find containers with runAsNonRoot=true",
			expression:     "spec.template.spec.containers.*.|securityContext.runAsNonRoot = true",
			expectedResult: api.ValidationResultTrue,
		},
		{
			name:           "Find containers without runAsNonRoot set to true",
			expression:     "spec.template.spec.containers.*.|securityContext.runAsNonRoot != true",
			expectedResult: api.ValidationResultTrue,
		},
		{
			name:           "Find containers with runAsNonRoot=false (should not match any)",
			expression:     "spec.template.spec.containers.*.|securityContext.runAsNonRoot = false",
			expectedResult: api.ValidationResultFalse,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			args := stringArgsToFunctionArgs([]string{"apps/v1/Deployment", tc.expression})
			_, result, err := k8sFnResourceWhereMatch(&fakeContext, docs, args, []byte{})
			assert.NoError(t, err)
			assert.Equal(t, tc.expectedResult, result, "Expression: %s", tc.expression)
		})
	}
}

func TestK8sFnResourceWhereMatch_SplitPathMissingProperty(t *testing.T) {
	yamlFixture := `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: test-deployment
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
      - name: container-no-security
        image: nginx:1.20
      - name: container-with-security
        image: nginx:1.20
        securityContext:
          runAsNonRoot: true
`
	docs, err := gaby.ParseAll([]byte(yamlFixture))
	assert.NoError(t, err)

	// Test that != evaluates to true for missing properties
	args := stringArgsToFunctionArgs([]string{"apps/v1/Deployment", "spec.template.spec.containers.*.|securityContext.runAsNonRoot != true"})
	_, result, err := k8sFnResourceWhereMatch(&fakeContext, docs, args, []byte{})
	assert.NoError(t, err)
	assert.Equal(t, api.ValidationResultTrue, result)

	// Test that = evaluates to false for missing properties
	args = stringArgsToFunctionArgs([]string{"apps/v1/Deployment", "spec.template.spec.containers.*.|securityContext.runAsNonRoot = false"})
	_, result, err = k8sFnResourceWhereMatch(&fakeContext, docs, args, []byte{})
	assert.NoError(t, err)
	assert.Equal(t, api.ValidationResultFalse, result)
}
