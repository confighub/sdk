// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package generic

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/confighub/sdk/configkit/k8skit"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/third_party/gaby"
)

var testResourceProvider = k8skit.NewK8sResourceProvider()

const deploymentFixture = `apiVersion: apps/v1
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

func stringArgsToFunctionArgs(args []string) []api.FunctionArgument {
	fa := make([]api.FunctionArgument, len(args))
	for i, s := range args {
		fa[i].Value = s
	}
	return fa
}

func TestSetStarlark_SetReplicas(t *testing.T) {
	docs, err := gaby.ParseAll([]byte(deploymentFixture))
	require.NoError(t, err)

	args := stringArgsToFunctionArgs([]string{
		`r["spec"]["replicas"] = 5`,
	})

	newDocs, _, err := genericFnSetStarlark(testResourceProvider, nil, docs, args)
	require.NoError(t, err)
	assert.Contains(t, newDocs.String(), "replicas: 5")
}

func TestSetStarlark_SetReplicasWithParams(t *testing.T) {
	docs, err := gaby.ParseAll([]byte(deploymentFixture))
	require.NoError(t, err)

	args := stringArgsToFunctionArgs([]string{
		`r["spec"]["replicas"] = int(params["replicas"])`,
		"replicas=7",
	})

	newDocs, _, err := genericFnSetStarlark(testResourceProvider, nil, docs, args)
	require.NoError(t, err)
	assert.Contains(t, newDocs.String(), "replicas: 7")
}

func TestSetStarlark_ObjectAlias(t *testing.T) {
	docs, err := gaby.ParseAll([]byte(deploymentFixture))
	require.NoError(t, err)

	args := stringArgsToFunctionArgs([]string{
		`object["spec"]["replicas"] = 10`,
	})

	newDocs, _, err := genericFnSetStarlark(testResourceProvider, nil, docs, args)
	require.NoError(t, err)
	assert.Contains(t, newDocs.String(), "replicas: 10")
}

func TestGetStarlark_GetReplicas(t *testing.T) {
	docs, err := gaby.ParseAll([]byte(deploymentFixture))
	require.NoError(t, err)

	args := stringArgsToFunctionArgs([]string{
		`def extract(r):
  return [{
    "ResourceName": r["metadata"].get("namespace", "") + "/" + r["metadata"]["name"],
    "ResourceType": r["apiVersion"] + "/" + r["kind"],
    "Path": "spec.replicas",
    "Value": r["spec"]["replicas"],
  }]`,
	})

	_, output, err := genericFnGetStarlark(testResourceProvider, nil, docs, args)
	require.NoError(t, err)

	attrValues, ok := output.(api.AttributeValueList)
	require.True(t, ok)
	require.Len(t, attrValues, 1)
	assert.Equal(t, api.ResourceName("default/nginx"), attrValues[0].ResourceName)
	assert.Equal(t, api.ResourceType("apps/v1/Deployment"), attrValues[0].ResourceType)
	assert.Equal(t, api.ResolvedPath("spec.replicas"), attrValues[0].Path)
	assert.EqualValues(t, 3, attrValues[0].Value)
}

func TestSetStarlark_PreservesComments(t *testing.T) {
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
		`r["spec"]["replicas"] = 5`,
	})

	newDocs, _, err := genericFnSetStarlark(testResourceProvider, nil, docs, args)
	require.NoError(t, err)

	output := newDocs.String()
	assert.Contains(t, output, "replicas: 5")
	assert.Contains(t, output, "# Number of pod replicas")
	assert.Contains(t, output, "# Main application container")
}

func TestVetStarlark_ReplicasMatchesParam(t *testing.T) {
	docs, err := gaby.ParseAll([]byte(deploymentFixture))
	require.NoError(t, err)

	vetProgram := `def validate(r):
  expected = int(params["replicas"])
  actual = r["spec"].get("replicas", 0)
  if actual != expected:
    name = r["metadata"].get("namespace", "") + "/" + r["metadata"]["name"]
    return {
      "passed": False,
      "details": [name + ": expected replicas " + str(expected) + " but got " + str(actual)],
      "failed_attributes": [{
        "ResourceName": name,
        "ResourceType": r["apiVersion"] + "/" + r["kind"],
        "Path": "spec.replicas",
        "Value": actual,
      }],
    }
  return {"passed": True}`

	t.Run("passes when replicas matches param", func(t *testing.T) {
		args := stringArgsToFunctionArgs([]string{vetProgram, "replicas=3"})

		_, output, err := genericFnVetStarlark(testResourceProvider, nil, docs, args)
		require.NoError(t, err)

		vr, ok := output.(api.ValidationResult)
		require.True(t, ok)
		assert.True(t, vr.Passed)
		assert.Empty(t, vr.Details)
		assert.Empty(t, vr.FailedAttributes)
	})

	t.Run("fails when replicas does not match param", func(t *testing.T) {
		args := stringArgsToFunctionArgs([]string{vetProgram, "replicas=5"})

		_, output, err := genericFnVetStarlark(testResourceProvider, nil, docs, args)
		require.NoError(t, err)

		vr, ok := output.(api.ValidationResult)
		require.True(t, ok)
		assert.False(t, vr.Passed)
		require.Len(t, vr.Details, 1)
		assert.Contains(t, vr.Details[0], "expected replicas 5 but got 3")
		require.Len(t, vr.FailedAttributes, 1)
		assert.Equal(t, api.ResourceName("default/nginx"), vr.FailedAttributes[0].ResourceName)
		assert.Equal(t, api.ResourceType("apps/v1/Deployment"), vr.FailedAttributes[0].ResourceType)
		assert.Equal(t, api.ResolvedPath("spec.replicas"), vr.FailedAttributes[0].Path)
		assert.EqualValues(t, 3, vr.FailedAttributes[0].Value)
	})
}

const deploymentAndServiceFixture = `apiVersion: apps/v1
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
---
apiVersion: v1
kind: Service
metadata:
  name: nginx
  namespace: default
spec:
  selector:
    app: nginx
  ports:
  - port: 80
    targetPort: 8080
`

func TestSetStarlark_WhereResourceFiltersResources(t *testing.T) {
	docs, err := gaby.ParseAll([]byte(deploymentAndServiceFixture))
	require.NoError(t, err)

	// This program reads spec.template which only exists on Deployment.
	// Without filtering, the Service would cause a key error.
	args := stringArgsToFunctionArgs([]string{
		`r["spec"]["template"]["metadata"]["labels"]["version"] = "v2"`,
	})

	// First verify that without filtering it fails on the Service
	_, _, err = genericFnSetStarlark(testResourceProvider, nil, docs, args)
	require.Error(t, err, "should fail without WhereResource filter because Service has no spec.template")

	// Now filter to only Deployments
	options, err := api.ParseAndValidateWhereResource("kind = 'Deployment'")
	require.NoError(t, err)

	newDocs, _, err := genericFnSetStarlark(testResourceProvider, options, docs, args)
	require.NoError(t, err)

	output := newDocs.String()
	// Deployment should be mutated
	assert.Contains(t, output, "version: v2")
	// Service should still be present and unchanged
	assert.Contains(t, output, "kind: Service")
	assert.Contains(t, output, "targetPort: 8080")
}

func TestVetStarlark_WhereResourceFiltersResources(t *testing.T) {
	docs, err := gaby.ParseAll([]byte(deploymentAndServiceFixture))
	require.NoError(t, err)

	vetProgram := `def validate(r):
  replicas = r["spec"].get("replicas", 0)
  if replicas < 1:
    return {"passed": False, "details": ["replicas must be >= 1"]}
  return {"passed": True}`

	args := stringArgsToFunctionArgs([]string{vetProgram})

	// Without filtering, Service has no replicas so it would fail validation
	_, output, err := genericFnVetStarlark(testResourceProvider, nil, docs, args)
	require.NoError(t, err)
	vr := output.(api.ValidationResult)
	assert.False(t, vr.Passed, "should fail without filter because Service has no replicas")

	// With filter, only Deployment is validated
	options, err := api.ParseAndValidateWhereResource("kind = 'Deployment'")
	require.NoError(t, err)

	_, output, err = genericFnVetStarlark(testResourceProvider, options, docs, args)
	require.NoError(t, err)
	vr = output.(api.ValidationResult)
	assert.True(t, vr.Passed, "should pass with filter targeting only Deployment")
}

func TestGetStarlark_WhereResourceFiltersResources(t *testing.T) {
	docs, err := gaby.ParseAll([]byte(deploymentAndServiceFixture))
	require.NoError(t, err)

	// This program accesses spec.replicas which only exists on Deployment.
	extractProgram := `def extract(r):
  return [{
    "ResourceName": r["metadata"].get("namespace", "") + "/" + r["metadata"]["name"],
    "ResourceType": r["apiVersion"] + "/" + r["kind"],
    "Path": "spec.replicas",
    "Value": r["spec"]["replicas"],
  }]`
	args := stringArgsToFunctionArgs([]string{extractProgram})

	// Without filtering, Service would cause a key error
	_, _, err = genericFnGetStarlark(testResourceProvider, nil, docs, args)
	require.Error(t, err, "should fail without WhereResource filter because Service has no spec.replicas")

	// With filter, only Deployment is processed
	options, err := api.ParseAndValidateWhereResource("kind = 'Deployment'")
	require.NoError(t, err)

	_, output, err := genericFnGetStarlark(testResourceProvider, options, docs, args)
	require.NoError(t, err)

	attrValues, ok := output.(api.AttributeValueList)
	require.True(t, ok)
	require.Len(t, attrValues, 1)
	assert.Equal(t, api.ResourceName("default/nginx"), attrValues[0].ResourceName)
	assert.Equal(t, api.ResourceType("apps/v1/Deployment"), attrValues[0].ResourceType)
	assert.EqualValues(t, 3, attrValues[0].Value)
}
