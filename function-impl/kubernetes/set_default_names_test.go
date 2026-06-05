// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package kubernetes

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/confighub/sdk/core/function/api"
)

// invokeSetDefaultNames runs set-default-names with the given name over the input
// config and returns the mutated config.
func invokeSetDefaultNames(t *testing.T, input, name string) string {
	t.Helper()
	req := &api.FunctionInvocationRequest{
		ConfigData: []byte(input),
		FunctionInvocations: []api.FunctionInvocation{
			{
				FunctionName: "set-default-names",
				Arguments:    []api.FunctionArgument{{ParameterName: "name", Value: name}},
			},
		},
	}
	resp, err := testFunctionHandler.InvokeCore(context.Background(), req)
	require.NoError(t, err)
	require.True(t, resp.Success, "set-default-names should succeed; errors: %v", resp.ErrorMessages)
	return string(resp.ConfigData)
}

// TestSetDefaultNames_ServiceLabelAndSelector verifies that set-default-names rewrites
// the placeholder app label and selector on a Service, not just on workload controllers.
// A Service selects pods via spec.selector (a plain label map), so both
// metadata.labels.app and spec.selector.app must be defaulted.
func TestSetDefaultNames_ServiceLabelAndSelector(t *testing.T) {
	const input = `apiVersion: v1
kind: Service
metadata:
  name: confighubplaceholder
  namespace: ui-preview
  labels:
    app: confighubplaceholder
spec:
  type: ClusterIP
  ports:
  - name: http
    port: 80
    targetPort: 8080
  selector:
    app: confighubplaceholder
`
	out := invokeSetDefaultNames(t, input, "pr-8292")

	assert.Contains(t, out, "name: pr-8292", "metadata.name should be defaulted")
	assert.NotContains(t, out, "confighubplaceholder",
		"no placeholder should remain in the Service label or selector")
	// Both the label and the selector must carry the new name.
	assert.Equal(t, 2, strings.Count(out, "app: pr-8292"),
		"expected both metadata.labels.app and spec.selector.app to be set to pr-8292")
}

// TestSetDefaultNames_NamedPlaceholderSuffixPreserved verifies that
// set-default-names replaces the placeholder in place rather than overwriting the
// whole field, so that a non-placeholder suffix on the name is preserved.
func TestSetDefaultNames_NamedPlaceholderSuffixPreserved(t *testing.T) {
	const input = `apiVersion: traefik.io/v1alpha1
kind: IngressRoute
metadata:
  name: confighubplaceholder-http
  namespace: ui-preview
`
	out := invokeSetDefaultNames(t, input, "pr-8292")

	assert.Contains(t, out, "name: pr-8292-http",
		"the -http suffix should be preserved after replacing the placeholder")
	assert.NotContains(t, out, "confighubplaceholder",
		"no placeholder should remain")
}

// TestSetDefaultNames_UIPreviewDistinctIngressRouteNames runs set-default-names
// over the test-data/ui-preview.yaml fixture and verifies that the two
// IngressRoute resources, whose placeholder names differ only by their -http and
// -https suffixes, still have distinct names after replacement. Replacing the
// whole field (the old behavior) would have collapsed them to the same name.
func TestSetDefaultNames_UIPreviewDistinctIngressRouteNames(t *testing.T) {
	data, err := os.ReadFile("../../../test-data/ui-preview.yaml")
	require.NoError(t, err)

	out := invokeSetDefaultNames(t, string(data), "pr-8292")

	// set-default-names only targets registered name fields, so placeholders in
	// other fields (image tags, route match hosts, etc.) are intentionally left
	// untouched. What matters here is that the two IngressRoute metadata.name
	// values stay distinct after replacement.
	assert.Contains(t, out, "name: pr-8292-http",
		"the http IngressRoute should keep its -http suffix")
	assert.Contains(t, out, "name: pr-8292-https",
		"the https IngressRoute should keep its -https suffix")
	assert.Equal(t, 1, strings.Count(out, "name: pr-8292-http\n"),
		"the http IngressRoute name should be unique")
	assert.Equal(t, 1, strings.Count(out, "name: pr-8292-https\n"),
		"the https IngressRoute name should be unique")
}
