// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package generic

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/confighub/sdk/configkit/k8skit"
	"github.com/confighub/sdk/function/api"
	"github.com/confighub/sdk/third_party/gaby"
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

	newDocs, _, err := genericFnSetStarlark(testResourceProvider, docs, args)
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

	newDocs, _, err := genericFnSetStarlark(testResourceProvider, docs, args)
	require.NoError(t, err)
	assert.Contains(t, newDocs.String(), "replicas: 7")
}

func TestSetStarlark_ObjectAlias(t *testing.T) {
	docs, err := gaby.ParseAll([]byte(deploymentFixture))
	require.NoError(t, err)

	args := stringArgsToFunctionArgs([]string{
		`object["spec"]["replicas"] = 10`,
	})

	newDocs, _, err := genericFnSetStarlark(testResourceProvider, docs, args)
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

	_, output, err := genericFnGetStarlark(testResourceProvider, docs, args)
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

	newDocs, _, err := genericFnSetStarlark(testResourceProvider, docs, args)
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

		_, output, err := genericFnVetStarlark(testResourceProvider, docs, args)
		require.NoError(t, err)

		vr, ok := output.(api.ValidationResult)
		require.True(t, ok)
		assert.True(t, vr.Passed)
		assert.Empty(t, vr.Details)
		assert.Empty(t, vr.FailedAttributes)
	})

	t.Run("fails when replicas does not match param", func(t *testing.T) {
		args := stringArgsToFunctionArgs([]string{vetProgram, "replicas=5"})

		_, output, err := genericFnVetStarlark(testResourceProvider, docs, args)
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
