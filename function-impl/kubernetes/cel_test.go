// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package kubernetes

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/function-impl/generic"
	"github.com/confighub/sdk/core/third_party/gaby"
)

const celDeploymentFixture = `apiVersion: apps/v1
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
        resources:
          limits:
            cpu: "500m"
            memory: "128Mi"
          requests:
            cpu: "250m"
            memory: "64Mi"
`

func TestK8sVetCEL_BoolPass(t *testing.T) {
	docs, err := gaby.ParseAll([]byte(celDeploymentFixture))
	require.NoError(t, err)

	args := stringArgsToFunctionArgs([]string{
		`object.spec.replicas == 3`,
	})

	_, output, err := generic.GenericFnVetCEL(testResourceProvider, nil, docs, args, k8sCELEnvOpts()...)
	require.NoError(t, err)

	vr, ok := output.(api.ValidationResult)
	require.True(t, ok)
	assert.True(t, vr.Passed)
}

func TestK8sVetCEL_BoolFail(t *testing.T) {
	docs, err := gaby.ParseAll([]byte(celDeploymentFixture))
	require.NoError(t, err)

	args := stringArgsToFunctionArgs([]string{
		`object.spec.replicas > 10`,
	})

	_, output, err := generic.GenericFnVetCEL(testResourceProvider, nil, docs, args, k8sCELEnvOpts()...)
	require.NoError(t, err)

	vr, ok := output.(api.ValidationResult)
	require.True(t, ok)
	assert.False(t, vr.Passed)
}

func TestK8sVetCEL_QuantityLibrary(t *testing.T) {
	docs, err := gaby.ParseAll([]byte(celDeploymentFixture))
	require.NoError(t, err)

	t.Run("memory limit >= 32Mi passes", func(t *testing.T) {
		args := stringArgsToFunctionArgs([]string{
			`object.kind != 'Deployment' || object.spec.template.spec.containers.all(c, quantity(c.resources.limits.memory).isGreaterThan(quantity('32Mi')))`,
		})

		_, output, err := generic.GenericFnVetCEL(testResourceProvider, nil, docs, args, k8sCELEnvOpts()...)
		require.NoError(t, err)

		vr, ok := output.(api.ValidationResult)
		require.True(t, ok)
		assert.True(t, vr.Passed)
	})

	t.Run("memory limit >= 256Mi fails", func(t *testing.T) {
		args := stringArgsToFunctionArgs([]string{
			`object.kind != 'Deployment' || object.spec.template.spec.containers.all(c, quantity(c.resources.limits.memory).isGreaterThan(quantity('256Mi')))`,
		})

		_, output, err := generic.GenericFnVetCEL(testResourceProvider, nil, docs, args, k8sCELEnvOpts()...)
		require.NoError(t, err)

		vr, ok := output.(api.ValidationResult)
		require.True(t, ok)
		assert.False(t, vr.Passed)
	})
}

func TestK8sVetCEL_MapResultWithDetails(t *testing.T) {
	docs, err := gaby.ParseAll([]byte(celDeploymentFixture))
	require.NoError(t, err)

	args := stringArgsToFunctionArgs([]string{
		`object.spec.replicas > 5 ? {"passed": true} : {"passed": false, "details": [object.metadata.name + " has only " + string(object.spec.replicas) + " replicas"]}`,
	})

	_, output, err := generic.GenericFnVetCEL(testResourceProvider, nil, docs, args, k8sCELEnvOpts()...)
	require.NoError(t, err)

	vr, ok := output.(api.ValidationResult)
	require.True(t, ok)
	assert.False(t, vr.Passed)
	require.Len(t, vr.Details, 1)
	assert.Contains(t, vr.Details[0], "nginx has only 3 replicas")
}

func TestK8sVetCEL_WithParams(t *testing.T) {
	docs, err := gaby.ParseAll([]byte(celDeploymentFixture))
	require.NoError(t, err)

	args := stringArgsToFunctionArgs([]string{
		`object.spec.replicas <= int(params.maxReplicas)`,
		"maxReplicas=10",
	})

	_, output, err := generic.GenericFnVetCEL(testResourceProvider, nil, docs, args, k8sCELEnvOpts()...)
	require.NoError(t, err)

	vr, ok := output.(api.ValidationResult)
	require.True(t, ok)
	assert.True(t, vr.Passed)
}

func TestK8sGetCEL_ExtractReplicas(t *testing.T) {
	docs, err := gaby.ParseAll([]byte(celDeploymentFixture))
	require.NoError(t, err)

	args := stringArgsToFunctionArgs([]string{
		`[{"ResourceName": object.metadata.namespace + "/" + object.metadata.name, "ResourceType": object.apiVersion + "/" + object.kind, "Path": "spec.replicas", "Value": object.spec.replicas}]`,
	})

	_, output, err := generic.GenericFnGetCEL(testResourceProvider, nil, docs, args, k8sCELEnvOpts()...)
	require.NoError(t, err)

	attrValues, ok := output.(api.AttributeValueList)
	require.True(t, ok)
	require.Len(t, attrValues, 1)
	assert.Equal(t, api.ResourceName("default/nginx"), attrValues[0].ResourceName)
	assert.Equal(t, api.ResourceType("apps/v1/Deployment"), attrValues[0].ResourceType)
	assert.Equal(t, api.ResolvedPath("spec.replicas"), attrValues[0].Path)
	assert.EqualValues(t, int64(3), attrValues[0].Value)
}

func TestK8sGetCEL_ExtractImages(t *testing.T) {
	docs, err := gaby.ParseAll([]byte(celDeploymentFixture))
	require.NoError(t, err)

	args := stringArgsToFunctionArgs([]string{
		`object.kind == "Deployment" ? object.spec.template.spec.containers.map(c, {"ResourceName": object.metadata.namespace + "/" + object.metadata.name, "ResourceType": object.apiVersion + "/" + object.kind, "Path": "spec.template.spec.containers." + c.name + ".image", "Value": c.image}) : []`,
	})

	_, output, err := generic.GenericFnGetCEL(testResourceProvider, nil, docs, args, k8sCELEnvOpts()...)
	require.NoError(t, err)

	attrValues, ok := output.(api.AttributeValueList)
	require.True(t, ok)
	require.Len(t, attrValues, 1)
	assert.Equal(t, "nginx:1.21", attrValues[0].Value)
	assert.Equal(t, api.ResolvedPath("spec.template.spec.containers.nginx.image"), attrValues[0].Path)
}

func TestK8sSetCEL_SetReplicas(t *testing.T) {
	docs, err := gaby.ParseAll([]byte(celDeploymentFixture))
	require.NoError(t, err)

	args := stringArgsToFunctionArgs([]string{
		`{"spec": {"replicas": 5}}`,
	})

	newDocs, _, err := generic.GenericFnSetCEL(testResourceProvider, docs, args, nil, k8sCELEnvOpts()...)
	require.NoError(t, err)
	output := newDocs.String()
	assert.Contains(t, output, "replicas: 5")
	assert.Contains(t, output, "kind: Deployment")
	assert.Contains(t, output, "image: nginx:1.21")
}

func TestK8sSetCEL_WithParams(t *testing.T) {
	docs, err := gaby.ParseAll([]byte(celDeploymentFixture))
	require.NoError(t, err)

	args := stringArgsToFunctionArgs([]string{
		`{"spec": {"replicas": int(params.replicas)}}`,
		"replicas=7",
	})

	newDocs, _, err := generic.GenericFnSetCEL(testResourceProvider, docs, args, nil, k8sCELEnvOpts()...)
	require.NoError(t, err)
	output := newDocs.String()
	assert.Contains(t, output, "replicas: 7")
	assert.Contains(t, output, "kind: Deployment")
}

func TestK8sVetCEL_UseObjectAlias(t *testing.T) {
	docs, err := gaby.ParseAll([]byte(celDeploymentFixture))
	require.NoError(t, err)

	// Both 'r' and 'object' should work
	args := stringArgsToFunctionArgs([]string{
		`r.spec.replicas == object.spec.replicas`,
	})

	_, output, err := generic.GenericFnVetCEL(testResourceProvider, nil, docs, args, k8sCELEnvOpts()...)
	require.NoError(t, err)

	vr, ok := output.(api.ValidationResult)
	require.True(t, ok)
	assert.True(t, vr.Passed)
}
