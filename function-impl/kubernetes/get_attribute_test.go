// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package kubernetes

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/confighub/sdk/core/function/api"
)

// deploymentWithEnv is a single-container Deployment with multiple env vars,
// used to exercise the env-value attribute, whose registered paths contain
// associative selectors for the container name and env var name. Resolving
// those requires the path-parameter keys passed to get-attribute.
const deploymentWithEnv = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
  namespace: default
spec:
  replicas: 3
  template:
    spec:
      containers:
        - name: app
          image: myapp:latest
          env:
            - name: DATABASE_URL
              value: postgres://localhost/main
            - name: LOG_LEVEL
              value: debug
`

// invokeGetAttribute runs a get-attribute invocation through the
// FunctionHandler (so that argument validation, including the varargs keys,
// is exercised) and returns the resulting AttributeValueList.
func invokeGetAttribute(t *testing.T, config string, args ...string) api.AttributeValueList {
	t.Helper()
	arguments := make([]api.FunctionArgument, len(args))
	for i, a := range args {
		arguments[i] = api.FunctionArgument{Value: a}
	}
	req := &api.FunctionInvocationRequest{
		ConfigData: []byte(config),
		FunctionInvocations: []api.FunctionInvocation{
			{
				FunctionName: "get-attribute",
				Arguments:    arguments,
			},
		},
	}
	resp, err := testFunctionHandler.InvokeCore(context.Background(), req)
	require.NoError(t, err)
	require.True(t, resp.Success, "function call should succeed; errors: %v", resp.ErrorMessages)

	out, ok := resp.Outputs[api.OutputTypeAttributeValueList]
	require.True(t, ok, "expected AttributeValueList output, got %v", resp.Outputs)

	var list api.AttributeValueList
	require.NoError(t, json.Unmarshal(out, &list))
	return list
}

// TestGetAttributeEnvValueWithKeys verifies that get-attribute resolves the
// env-value attribute when the container name and env var name are passed as
// path-parameter keys.
func TestGetAttributeEnvValueWithKeys(t *testing.T) {
	list := invokeGetAttribute(t, deploymentWithEnv, "env-value", "app", "DATABASE_URL")
	require.Len(t, list, 1)
	assert.Equal(t, "postgres://localhost/main", list[0].Value)

	list = invokeGetAttribute(t, deploymentWithEnv, "env-value", "app", "LOG_LEVEL")
	require.Len(t, list, 1)
	assert.Equal(t, "debug", list[0].Value)
}

// TestGetAttributeEnvValueWrongKey verifies that keys that don't match any
// env var resolve to no values rather than an error.
func TestGetAttributeEnvValueWrongKey(t *testing.T) {
	list := invokeGetAttribute(t, deploymentWithEnv, "env-value", "app", "NONEXISTENT")
	assert.Empty(t, list)
}

// TestGetAttributeNoKeys verifies backward compatibility: attributes whose
// registered paths contain no associative selectors still resolve when no
// keys are supplied.
func TestGetAttributeNoKeys(t *testing.T) {
	list := invokeGetAttribute(t, deploymentWithEnv, "replicas")
	require.Len(t, list, 1)
	assert.EqualValues(t, 3, list[0].Value)
}
