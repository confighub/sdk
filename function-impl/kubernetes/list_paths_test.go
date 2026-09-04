// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package kubernetes

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/confighub/sdk/core/function/api"
)

// deploymentForPaths carries the shapes the reported path has to get right: a
// merge-keyed container list, a nested merge-keyed env list, and a map key containing
// dots.
const deploymentForPaths = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
  namespace: default
  annotations:
    confighub.com/owner: platform
spec:
  replicas: 3
  template:
    spec:
      containers:
        - name: app
          image: myapp:latest
          env:
            - name: LOG_LEVEL
              value: debug
`

func invokeListPathsRaw(t *testing.T, config string, args ...api.FunctionArgument) *api.FunctionInvocationResponse {
	t.Helper()
	req := &api.FunctionInvocationRequest{
		ConfigData: config,
		FunctionInvocations: []api.FunctionInvocation{
			{
				FunctionName: "list-paths",
				Arguments:    args,
			},
		},
	}
	resp, err := testFunctionHandler.InvokeCore(context.Background(), req)
	require.NoError(t, err, "argument validation is reported in the response, not returned")
	return resp
}

// invokeListPaths runs list-paths through the FunctionHandler, so the registration, the
// argument validation, and the path registry the Kubernetes toolchain actually populates
// are all exercised. The generic package's own tests cover the walk itself.
func invokeListPaths(t *testing.T, config string, args ...api.FunctionArgument) api.AttributeValueList {
	t.Helper()
	resp := invokeListPathsRaw(t, config, args...)
	require.True(t, resp.Success, "function call should succeed; errors: %v", resp.ErrorMessages)

	out, ok := resp.Outputs[api.OutputTypeAttributeValueList]
	require.True(t, ok, "expected AttributeValueList output, got %v", resp.Outputs)

	var list api.AttributeValueList
	require.NoError(t, json.Unmarshal(out, &list))
	return list
}

func entriesByPath(list api.AttributeValueList) map[string]api.AttributeValue {
	byPath := make(map[string]api.AttributeValue, len(list))
	for _, entry := range list {
		byPath[string(entry.Path)] = entry
	}
	return byPath
}

// The reported path is the one the setters accept. Feeding each one back through
// get-path has to find the value list-paths reported at it, or the path is decorative.
func TestListPathsRoundTripThroughGetPath(t *testing.T) {
	list := invokeListPaths(t, deploymentForPaths)
	require.NotEmpty(t, list)

	for _, entry := range list {
		req := &api.FunctionInvocationRequest{
			ConfigData: deploymentForPaths,
			FunctionInvocations: []api.FunctionInvocation{
				{
					FunctionName: "get-path",
					Arguments:    []api.FunctionArgument{{Value: string(entry.Path)}},
				},
			},
		}
		resp, err := testFunctionHandler.InvokeCore(context.Background(), req)
		require.NoError(t, err, "get-path %s", entry.Path)
		require.True(t, resp.Success, "get-path %s should succeed; errors: %v", entry.Path, resp.ErrorMessages)

		var found api.AttributeValueList
		require.NoError(t, json.Unmarshal(resp.Outputs[api.OutputTypeAttributeValueList], &found))
		require.Len(t, found, 1, "get-path %s should find exactly one value", entry.Path)
		assert.Equal(t, entry.Value, found[0].Value, "get-path %s", entry.Path)
	}
}

// Against the registry the Kubernetes toolchain really populates, a path that a named
// setter owns is reported with that setter's attribute.
func TestListPathsReportsKubernetesAttributes(t *testing.T) {
	byPath := entriesByPath(invokeListPaths(t, deploymentForPaths))

	image, ok := byPath["spec.template.spec.containers.?name=app.image"]
	require.True(t, ok, "container image should be reported; got %v", byPath)
	assert.Equal(t, api.AttributeNameContainerImage, image.AttributeName)

	replicas, ok := byPath["spec.replicas"]
	require.True(t, ok)
	assert.Equal(t, api.AttributeName("replicas"), replicas.AttributeName)
	assert.Equal(t, float64(3), replicas.Value, "JSON round-trips numbers as float64")
}

// attributes-only answers "what can I set on this unit without knowing path syntax".
// It is the last parameter, and reaching it by name is how a caller skips the others.
func TestListPathsAttributesOnly(t *testing.T) {
	all := invokeListPaths(t, deploymentForPaths)
	bound := invokeListPaths(t, deploymentForPaths, api.FunctionArgument{ParameterName: "attributes-only", Value: true})

	require.NotEmpty(t, bound)
	assert.Less(t, len(bound), len(all), "attributes-only should be a strict subset here")
	for _, entry := range bound {
		assert.NotEqual(t, api.AttributeNameNone, entry.AttributeName, "%s", entry.Path)
	}
}

// Nested merge-keyed lists are named at every level.
func TestListPathsNamesNestedListElements(t *testing.T) {
	byPath := entriesByPath(invokeListPaths(t, deploymentForPaths))

	_, ok := byPath["spec.template.spec.containers.?name=app.env.?name=LOG_LEVEL.value"]
	assert.True(t, ok, "nested env element should be named by merge key; got %v", byPath)
}

// WhereResource narrows which resources are walked, since a unit holding a whole
// application is the case where the full listing is least usable.
func TestListPathsRespectsWhereResource(t *testing.T) {
	const twoResources = deploymentForPaths + `---
apiVersion: v1
kind: Service
metadata:
  name: web
  namespace: default
spec:
  clusterIP: 10.0.0.1
`
	req := &api.FunctionInvocationRequest{
		ConfigData: twoResources,
		FunctionInvocationOptions: api.FunctionInvocationOptions{
			WhereResource: "ConfigHub.ResourceType = 'v1/Service'",
		},
		FunctionInvocations: []api.FunctionInvocation{
			{FunctionName: "list-paths"},
		},
	}
	resp, err := testFunctionHandler.InvokeCore(context.Background(), req)
	require.NoError(t, err)
	require.True(t, resp.Success, "errors: %v", resp.ErrorMessages)

	var list api.AttributeValueList
	require.NoError(t, json.Unmarshal(resp.Outputs[api.OutputTypeAttributeValueList], &list))

	require.NotEmpty(t, list)
	for _, entry := range list {
		assert.Equal(t, api.ResourceType("v1/Service"), entry.ResourceType)
	}
}

// Positional arguments are named by the handler before the function sees them, so the
// two call styles select the same paths.
func TestListPathsPositionalAndNamedArgumentsAgree(t *testing.T) {
	positional := invokeListPaths(t, deploymentForPaths, api.FunctionArgument{Value: "spec.template"})
	named := invokeListPaths(t, deploymentForPaths, api.FunctionArgument{ParameterName: "path-prefix", Value: "spec.template"})

	require.NotEmpty(t, positional)
	assert.Equal(t, positional, named)
}

// The constraint rejects a value that cannot begin a path. Like every other path
// parameter, it anchors at the start only, so it catches a malformed opening rather than
// validating the whole expression.
func TestListPathsRejectsMalformedPathPrefix(t *testing.T) {
	resp := invokeListPathsRaw(t, deploymentForPaths, api.FunctionArgument{Value: ".spec.replicas"})
	require.False(t, resp.Success)
	assert.Contains(t, strings.Join(resp.ErrorMessages, "\n"), "path-prefix")
}
