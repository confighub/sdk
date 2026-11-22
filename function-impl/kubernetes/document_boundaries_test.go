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

// These tests run functions on Units whose document boundaries are unusual, since every
// function call reads and writes the Unit through the multi-document parser: what it misses
// is invisible to read-only functions and deleted by mutating ones.

func invokeOne(t *testing.T, data string, invocation api.FunctionInvocation) *api.FunctionInvocationResponse {
	t.Helper()
	resp, err := testFunctionHandler.InvokeCore(context.Background(), &api.FunctionInvocationRequest{
		ConfigData:          data,
		FunctionInvocations: []api.FunctionInvocation{invocation},
	})
	require.NoError(t, err)
	require.True(t, resp.Success, "errors: %v", resp.ErrorMessages)
	return resp
}

func setNamespace(t *testing.T, data string) *api.FunctionInvocationResponse {
	t.Helper()
	return invokeOne(t, data, api.FunctionInvocation{
		FunctionName: "set-namespace",
		Arguments:    []api.FunctionArgument{{Value: "team"}},
	})
}

func resourceNames(t *testing.T, data string) []string {
	t.Helper()
	resp := invokeOne(t, data, api.FunctionInvocation{FunctionName: "get-resources"})
	var list api.ResourceList
	if encoded, ok := resp.Outputs[api.OutputTypeResourceList]; ok {
		require.NoError(t, json.Unmarshal(encoded, &list))
	}
	names := []string{}
	for _, r := range list {
		names = append(names, string(r.ResourceNameWithoutScope))
	}
	return names
}

func TestCommentOnlyUnitIsLeftUnchanged(t *testing.T) {
	for name, data := range map[string]string{
		"comments":                   "# placeholder\n# to be filled in\n",
		"comments around separators": "---\n# placeholder\n---\n# more\n",
	} {
		t.Run(name, func(t *testing.T) {
			resp := setNamespace(t, data)
			assert.Empty(t, resp.Mutators, "a Unit with no resources has nothing to set")
			assert.Equal(t, api.HashConfigDataSHA256(data), resp.DataHash, "the data should be what was sent")
			assert.Empty(t, resp.ConfigData)
			assert.Empty(t, resourceNames(t, data))
		})
	}
}

func TestTrailingCommentOnlyDocumentIsKept(t *testing.T) {
	data := `apiVersion: v1
kind: ConfigMap
metadata:
  name: settings
---
# Add the second ConfigMap here.
`
	resp := setNamespace(t, data)
	assert.Contains(t, resp.ConfigData, "namespace: team")
	assert.Contains(t, resp.ConfigData, "# Add the second ConfigMap here.")
}

func TestDocumentsAfterAnUnusualSeparatorAreKept(t *testing.T) {
	configMap := func(name string) string {
		return "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: " + name + "\n"
	}
	for name, data := range map[string]string{
		"trailing space":    configMap("first") + "--- \n" + configMap("second"),
		"trailing tab":      configMap("first") + "---\t\n" + configMap("second"),
		"trailing comment":  configMap("first") + "--- # second\n" + configMap("second"),
		"document end":      configMap("first") + "...\n" + configMap("second"),
		"content on marker": configMap("first") + "--- {apiVersion: v1, kind: ConfigMap, metadata: {name: second}}\n",
	} {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, []string{"first", "second"}, resourceNames(t, data))
			resp := setNamespace(t, data)
			assert.Equal(t, []string{"team/first", "team/second"}, resourceNamesWithScope(t, resp.ConfigData))
		})
	}
}

func resourceNamesWithScope(t *testing.T, data string) []string {
	t.Helper()
	resp := invokeOne(t, data, api.FunctionInvocation{FunctionName: "get-resources"})
	var list api.ResourceList
	require.NoError(t, json.Unmarshal(resp.Outputs[api.OutputTypeResourceList], &list))
	names := []string{}
	for _, r := range list {
		names = append(names, string(r.ResourceName))
	}
	return names
}
