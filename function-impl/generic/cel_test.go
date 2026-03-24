// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package generic

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/third_party/gaby"
)

func TestVetCEL_BoolPass(t *testing.T) {
	docs, err := gaby.ParseAll([]byte(deploymentFixture))
	require.NoError(t, err)

	args := stringArgsToFunctionArgs([]string{
		`r.spec.replicas == 3`,
	})

	_, output, err := GenericFnVetCEL(testResourceProvider, nil, docs, args)
	require.NoError(t, err)

	vr, ok := output.(api.ValidationResult)
	require.True(t, ok)
	assert.True(t, vr.Passed)
}

func TestVetCEL_BoolFail(t *testing.T) {
	docs, err := gaby.ParseAll([]byte(deploymentFixture))
	require.NoError(t, err)

	args := stringArgsToFunctionArgs([]string{
		`r.spec.replicas == 5`,
	})

	_, output, err := GenericFnVetCEL(testResourceProvider, nil, docs, args)
	require.NoError(t, err)

	vr, ok := output.(api.ValidationResult)
	require.True(t, ok)
	assert.False(t, vr.Passed)
}

func TestVetCEL_MapResult(t *testing.T) {
	docs, err := gaby.ParseAll([]byte(deploymentFixture))
	require.NoError(t, err)

	args := stringArgsToFunctionArgs([]string{
		`r.spec.replicas > 5 ? {"passed": true} : {"passed": false, "details": [r.metadata.name + " has too few replicas"]}`,
	})

	_, output, err := GenericFnVetCEL(testResourceProvider, nil, docs, args)
	require.NoError(t, err)

	vr, ok := output.(api.ValidationResult)
	require.True(t, ok)
	assert.False(t, vr.Passed)
	require.Len(t, vr.Details, 1)
	assert.Contains(t, vr.Details[0], "nginx has too few replicas")
}

func TestVetCEL_WithParams(t *testing.T) {
	docs, err := gaby.ParseAll([]byte(deploymentFixture))
	require.NoError(t, err)

	t.Run("passes when replicas matches", func(t *testing.T) {
		args := stringArgsToFunctionArgs([]string{
			`r.spec.replicas == int(params.replicas)`,
			"replicas=3",
		})

		_, output, err := GenericFnVetCEL(testResourceProvider, nil, docs, args)
		require.NoError(t, err)

		vr, ok := output.(api.ValidationResult)
		require.True(t, ok)
		assert.True(t, vr.Passed)
	})

	t.Run("fails when replicas does not match", func(t *testing.T) {
		args := stringArgsToFunctionArgs([]string{
			`r.spec.replicas == int(params.replicas)`,
			"replicas=5",
		})

		_, output, err := GenericFnVetCEL(testResourceProvider, nil, docs, args)
		require.NoError(t, err)

		vr, ok := output.(api.ValidationResult)
		require.True(t, ok)
		assert.False(t, vr.Passed)
	})
}

func TestVetCEL_CompileError(t *testing.T) {
	docs, err := gaby.ParseAll([]byte(deploymentFixture))
	require.NoError(t, err)

	args := stringArgsToFunctionArgs([]string{
		`r.spec.replicas ===`,
	})

	_, output, err := GenericFnVetCEL(testResourceProvider, nil, docs, args)
	require.NoError(t, err) // compile errors are returned as failed validation, not errors

	vr, ok := output.(api.ValidationResult)
	require.True(t, ok)
	assert.False(t, vr.Passed)
	require.NotEmpty(t, vr.Details)
}

func TestGetCEL_ExtractReplicas(t *testing.T) {
	docs, err := gaby.ParseAll([]byte(deploymentFixture))
	require.NoError(t, err)

	args := stringArgsToFunctionArgs([]string{
		`[{"ResourceName": r.metadata.namespace + "/" + r.metadata.name, "ResourceType": r.apiVersion + "/" + r.kind, "Path": "spec.replicas", "Value": r.spec.replicas}]`,
	})

	_, output, err := GenericFnGetCEL(testResourceProvider, nil, docs, args)
	require.NoError(t, err)

	attrValues, ok := output.(api.AttributeValueList)
	require.True(t, ok)
	require.Len(t, attrValues, 1)
	assert.Equal(t, api.ResourceName("default/nginx"), attrValues[0].ResourceName)
	assert.Equal(t, api.ResourceType("apps/v1/Deployment"), attrValues[0].ResourceType)
	assert.Equal(t, api.ResolvedPath("spec.replicas"), attrValues[0].Path)
	assert.EqualValues(t, int64(3), attrValues[0].Value)
}

func TestGetCEL_ConditionalExtract(t *testing.T) {
	docs, err := gaby.ParseAll([]byte(deploymentFixture))
	require.NoError(t, err)

	args := stringArgsToFunctionArgs([]string{
		`r.kind == "Service" ? [{"ResourceName": r.metadata.name, "Path": "spec.type", "Value": r.spec.type}] : []`,
	})

	_, output, err := GenericFnGetCEL(testResourceProvider, nil, docs, args)
	require.NoError(t, err)

	attrValues, ok := output.(api.AttributeValueList)
	require.True(t, ok)
	assert.Len(t, attrValues, 0)
}

func TestSetCEL_SetReplicas(t *testing.T) {
	docs, err := gaby.ParseAll([]byte(deploymentFixture))
	require.NoError(t, err)

	// Only specify the field to change — it gets merged into the original
	args := stringArgsToFunctionArgs([]string{
		`{"spec": {"replicas": 5}}`,
	})

	newDocs, _, err := GenericFnSetCEL(testResourceProvider, docs, args)
	require.NoError(t, err)
	output := newDocs.String()
	assert.Contains(t, output, "replicas: 5")
	// Verify other fields are preserved
	assert.Contains(t, output, "kind: Deployment")
	assert.Contains(t, output, "name: nginx")
	assert.Contains(t, output, "image: nginx:1.21")
}

func TestSetCEL_WithParams(t *testing.T) {
	docs, err := gaby.ParseAll([]byte(deploymentFixture))
	require.NoError(t, err)

	args := stringArgsToFunctionArgs([]string{
		`{"spec": {"replicas": int(params.replicas)}}`,
		"replicas=7",
	})

	newDocs, _, err := GenericFnSetCEL(testResourceProvider, docs, args)
	require.NoError(t, err)
	output := newDocs.String()
	assert.Contains(t, output, "replicas: 7")
	assert.Contains(t, output, "kind: Deployment")
}

func TestSetCEL_PreservesComments(t *testing.T) {
	fixture := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: nginx
  namespace: default
spec:
  # Number of pod replicas
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
      # Main application container
      - name: nginx
        image: nginx:1.21
`
	docs, err := gaby.ParseAll([]byte(fixture))
	require.NoError(t, err)

	args := stringArgsToFunctionArgs([]string{
		`{"spec": {"replicas": 5}}`,
	})

	newDocs, _, err := GenericFnSetCEL(testResourceProvider, docs, args)
	require.NoError(t, err)

	output := newDocs.String()
	assert.Contains(t, output, "replicas: 5")
	assert.Contains(t, output, "# Number of pod replicas")
	assert.Contains(t, output, "# Main application container")
}

func TestSetCEL_MergePreservesUnspecifiedFields(t *testing.T) {
	docs, err := gaby.ParseAll([]byte(deploymentFixture))
	require.NoError(t, err)

	// Only change replicas — all other fields should be preserved exactly
	args := stringArgsToFunctionArgs([]string{
		`{"spec": {"replicas": 10}}`,
	})

	newDocs, _, err := GenericFnSetCEL(testResourceProvider, docs, args)
	require.NoError(t, err)

	output := newDocs.String()
	assert.Contains(t, output, "replicas: 10")
	assert.Contains(t, output, "apiVersion: apps/v1")
	assert.Contains(t, output, "kind: Deployment")
	assert.Contains(t, output, "namespace: default")
	assert.Contains(t, output, "app: nginx")
	assert.Contains(t, output, "image: nginx:1.21")
}

func TestVetCEL_SkipsNonMatchingResources(t *testing.T) {
	docs, err := gaby.ParseAll([]byte(deploymentFixture))
	require.NoError(t, err)

	// Should pass because expression returns true for non-Deployments, and also for this deployment
	args := stringArgsToFunctionArgs([]string{
		`r.kind != "Deployment" || r.spec.replicas >= 1`,
	})

	_, output, err := GenericFnVetCEL(testResourceProvider, nil, docs, args)
	require.NoError(t, err)

	vr, ok := output.(api.ValidationResult)
	require.True(t, ok)
	assert.True(t, vr.Passed)
}
