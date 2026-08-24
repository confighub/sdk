// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package kubernetes

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/confighub/sdk/core/function/api"
)

// twoDeploymentsAndService is a three-resource Unit used to exercise
// request-level WhereResource, per-invocation WhereResource, and the
// AND-combination of the two through the FunctionHandler.
const twoDeploymentsAndService = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
  namespace: default
spec:
  replicas: 3
  template:
    spec:
      containers:
        - name: web
          image: nginx:1.27
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: api
  namespace: default
spec:
  replicas: 2
  template:
    spec:
      containers:
        - name: app
          image: myapp:latest
---
apiVersion: v1
kind: Service
metadata:
  name: web-svc
  namespace: default
spec:
  type: ClusterIP
  ports:
    - port: 80
`

// invokeGetResourcesOfType runs the request through the FunctionHandler and
// returns the sorted set of resource names from a ResourceInfoList output.
// Used to verify which resources passed through the WhereResource filter
// chain.
func invokeGetResourcesOfType(t *testing.T, req *api.FunctionInvocationRequest) []string {
	t.Helper()
	resp, err := testFunctionHandler.InvokeCore(context.Background(), req)
	require.NoError(t, err)
	require.True(t, resp.Success, "function call should succeed; errors: %v", resp.ErrorMessages)

	out, ok := resp.Outputs[api.OutputTypeResourceInfoList]
	require.True(t, ok, "expected ResourceInfoList output, got %v", resp.Outputs)

	var list api.ResourceInfoList
	require.NoError(t, json.Unmarshal(out, &list))

	names := make([]string, 0, len(list))
	for _, ri := range list {
		names = append(names, string(ri.ResourceName))
	}
	sort.Strings(names)
	return names
}

// TestHandlerWhereResource_RequestLevelOnly verifies the request-level
// WhereResource filters by ConfigHub.ResourceType — Deployments pass,
// Service is excluded.
func TestHandlerWhereResource_RequestLevelOnly(t *testing.T) {
	req := &api.FunctionInvocationRequest{
		ConfigData: twoDeploymentsAndService,
		FunctionInvocationOptions: api.FunctionInvocationOptions{
			WhereResource: "ConfigHub.ResourceType = 'apps/v1/Deployment'",
		},
		FunctionInvocations: []api.FunctionInvocation{
			{
				FunctionName: "get-resources-of-type",
				Arguments:    []api.FunctionArgument{{Value: "apps/v1/Deployment"}},
			},
		},
	}
	names := invokeGetResourcesOfType(t, req)
	assert.Equal(t, []string{"default/api", "default/web"}, names)
}

// TestHandlerWhereResource_InvocationLevelOnly verifies that a
// per-invocation WhereResource without any request-level filter narrows
// the visible resource set to just the matching resource.
func TestHandlerWhereResource_InvocationLevelOnly(t *testing.T) {
	req := &api.FunctionInvocationRequest{
		ConfigData: twoDeploymentsAndService,
		FunctionInvocations: []api.FunctionInvocation{
			{
				FunctionName:  "get-resources-of-type",
				Arguments:     []api.FunctionArgument{{Value: "apps/v1/Deployment"}},
				WhereResource: "ConfigHub.ResourceName = 'default/web'",
			},
		},
	}
	names := invokeGetResourcesOfType(t, req)
	assert.Equal(t, []string{"default/web"}, names)
}

// TestHandlerWhereResource_Combined verifies AND semantics: a
// request-level filter on ResourceType combined with a per-invocation
// filter on ResourceName narrows the set to resources matching both.
func TestHandlerWhereResource_Combined(t *testing.T) {
	req := &api.FunctionInvocationRequest{
		ConfigData: twoDeploymentsAndService,
		FunctionInvocationOptions: api.FunctionInvocationOptions{
			WhereResource: "ConfigHub.ResourceType = 'apps/v1/Deployment'",
		},
		FunctionInvocations: []api.FunctionInvocation{
			{
				FunctionName:  "get-resources-of-type",
				Arguments:     []api.FunctionArgument{{Value: "apps/v1/Deployment"}},
				WhereResource: "ConfigHub.ResourceName = 'default/api'",
			},
		},
	}
	names := invokeGetResourcesOfType(t, req)
	assert.Equal(t, []string{"default/api"}, names)
}

// TestHandlerWhereResource_PerInvocationDistinctScopes verifies that two
// invocations in the same request each apply their own WhereResource
// independently — function 0's filter does not leak into function 1.
// Uses get-cel because its AttributeValueList output carries the
// per-invocation Index that the combined output preserves.
func TestHandlerWhereResource_PerInvocationDistinctScopes(t *testing.T) {
	getCelArg := `[{"ResourceName": r.metadata.?namespace.orValue("") + "/" + r.metadata.name, "ResourceType": r.apiVersion + "/" + r.kind, "Path": "metadata.name", "Value": r.metadata.name}]`
	req := &api.FunctionInvocationRequest{
		ConfigData: twoDeploymentsAndService,
		FunctionInvocations: []api.FunctionInvocation{
			{
				FunctionName:  "get-cel",
				Arguments:     []api.FunctionArgument{{Value: getCelArg}},
				WhereResource: "ConfigHub.ResourceName = 'default/web'",
			},
			{
				FunctionName:  "get-cel",
				Arguments:     []api.FunctionArgument{{Value: getCelArg}},
				WhereResource: "ConfigHub.ResourceName = 'default/api'",
			},
		},
	}
	resp, err := testFunctionHandler.InvokeCore(context.Background(), req)
	require.NoError(t, err)
	require.True(t, resp.Success, "function call should succeed; errors: %v", resp.ErrorMessages)

	out, ok := resp.Outputs[api.OutputTypeAttributeValueList]
	require.True(t, ok)
	var list api.AttributeValueList
	require.NoError(t, json.Unmarshal(out, &list))

	byIndex := map[int][]string{}
	for _, av := range list {
		byIndex[av.Index] = append(byIndex[av.Index], string(av.ResourceName))
	}
	for k := range byIndex {
		sort.Strings(byIndex[k])
	}
	assert.Equal(t, []string{"default/web"}, byIndex[0])
	assert.Equal(t, []string{"default/api"}, byIndex[1])
}

// TestHandlerWhereResource_InvalidInvocationLevel verifies the handler
// surfaces an error when a per-invocation WhereResource fails to parse,
// rather than silently ignoring it.
func TestHandlerWhereResource_InvalidInvocationLevel(t *testing.T) {
	req := &api.FunctionInvocationRequest{
		ConfigData: twoDeploymentsAndService,
		FunctionInvocationOptions: api.FunctionInvocationOptions{
			StopOnError: true,
		},
		FunctionInvocations: []api.FunctionInvocation{
			{
				FunctionName:  "get-resources-of-type",
				Arguments:     []api.FunctionArgument{{Value: "apps/v1/Deployment"}},
				WhereResource: "ConfigHub.Bogus = 'x'",
			},
		},
	}
	resp, err := testFunctionHandler.InvokeCore(context.Background(), req)
	require.NoError(t, err)
	require.False(t, resp.Success, "invalid WhereResource should fail the invocation")
	require.NotEmpty(t, resp.ErrorMessages)
	assert.True(t, strings.Contains(resp.ErrorMessages[0], "WhereResource") ||
		strings.Contains(resp.ErrorMessages[0], "ConfigHub.Bogus"),
		"error message should mention the bad WhereResource: %v", resp.ErrorMessages)
}
