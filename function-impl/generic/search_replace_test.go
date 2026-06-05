// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package generic

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/confighub/sdk/configkit/k8skit"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/third_party/gaby"
)

const searchReplaceYAML = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
  namespace: default
spec:
  template:
    spec:
      containers:
      - name: web
        image: ghcr.io/example/web:v1.2.3
      - name: sidecar
        image: ghcr.io/example/sidecar:v1.2.3
`

// invokeSearchReplace runs genericFnSearchReplace over the input config and
// returns the mutated config as a YAML string.
func invokeSearchReplace(t *testing.T, input string, args []api.FunctionArgument) string {
	t.Helper()
	rp := k8skit.NewK8sResourceProvider()
	parsedData, err := gaby.ParseAll([]byte(input))
	require.NoError(t, err)

	result, _, err := genericFnSearchReplace(rp, nil, parsedData, args, nil)
	require.NoError(t, err)
	return result.String()
}

// TestSearchReplace_Literal verifies literal substring replacement, the default
// behavior when the regexp argument is omitted.
func TestSearchReplace_Literal(t *testing.T) {
	out := invokeSearchReplace(t, searchReplaceYAML, []api.FunctionArgument{
		{ParameterName: "search-value", Value: "v1.2.3"},
		{ParameterName: "replace-value", Value: "v2.0.0"},
	})

	assert.NotContains(t, out, "v1.2.3", "the old tag should be gone")
	assert.Equal(t, 2, strings.Count(out, ":v2.0.0"),
		"both container image tags should be replaced")
}

// TestSearchReplace_Regexp verifies regular-expression matching with sed-like
// submatch expansion in the replacement.
func TestSearchReplace_Regexp(t *testing.T) {
	out := invokeSearchReplace(t, searchReplaceYAML, []api.FunctionArgument{
		// Capture the major version and rewrite the whole tag, bumping it.
		{ParameterName: "search-value", Value: `:v(\d+)\.\d+\.\d+`},
		{ParameterName: "replace-value", Value: ":v${1}.0.0"},
		{ParameterName: "regexp", Value: true},
	})

	assert.NotContains(t, out, "v1.2.3", "the old tag should be gone")
	assert.Equal(t, 2, strings.Count(out, ":v1.0.0"),
		"both image tags should be rewritten using the captured major version")
}

// TestSearchReplace_RegexpInvalid verifies that an invalid regular expression
// surfaces an error instead of silently doing nothing.
func TestSearchReplace_RegexpInvalid(t *testing.T) {
	rp := k8skit.NewK8sResourceProvider()
	parsedData, err := gaby.ParseAll([]byte(searchReplaceYAML))
	require.NoError(t, err)

	args := []api.FunctionArgument{
		{ParameterName: "search-value", Value: "v1.2.3("},
		{ParameterName: "replace-value", Value: "x"},
		{ParameterName: "regexp", Value: true},
	}
	_, _, err = genericFnSearchReplace(rp, nil, parsedData, args, nil)
	require.Error(t, err, "an invalid regular expression should produce an error")
}
