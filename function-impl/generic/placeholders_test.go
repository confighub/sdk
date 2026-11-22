// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package generic

import (
	"testing"

	"github.com/confighub/sdk/configkit/propkit"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/third_party/gaby"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// In a format whose values are all text, the placeholder number is text, which
// get-placeholders, get-placeholder-mutations and vet-placeholders still find.
func TestPlaceholdersInTextFormats(t *testing.T) {
	rp := propkit.NewPropertiesResourceProvider()
	parse := func() gaby.Container {
		y, err := rp.NativeToYAML([]byte("server.port=999999999\nserver.host=confighubplaceholder\nserver.name=app\nserver.retries=99999999\n"))
		require.NoError(t, err)
		docs, err := gaby.ParseAll(y)
		require.NoError(t, err)
		return docs
	}
	want := []string{"server.port", "server.host"}
	pathsOf := func(list api.AttributeValueList) []string {
		var paths []string
		for _, attribute := range list {
			paths = append(paths, string(attribute.Path))
		}
		return paths
	}

	_, output, err := genericFnVetPlaceholders(rp, nil, parse(), nil, &api.FunctionOptions{})
	require.NoError(t, err)
	result, ok := output.(api.ValidationResult)
	require.True(t, ok)
	assert.False(t, result.Passed)
	assert.ElementsMatch(t, want, pathsOf(result.FailedAttributes))

	_, output, err = genericFnGetPlaceholders(rp, nil, parse(), nil, &api.FunctionOptions{})
	require.NoError(t, err)
	found, ok := output.(api.AttributeValueList)
	require.True(t, ok)
	assert.ElementsMatch(t, want, pathsOf(found))

	_, output, err = genericFnGetPlaceholderMutations(rp, nil, parse(), nil, &api.FunctionOptions{})
	require.NoError(t, err)
	mutations, ok := output.(api.ResourceMutationList)
	require.True(t, ok)
	var mutated []string
	for _, mutation := range mutations {
		for path := range mutation.PathMutationMap {
			mutated = append(mutated, string(path))
		}
	}
	assert.ElementsMatch(t, want, mutated)
}

// A placeholder number in a typed format is found only as a number: the string "999999999" in
// YAML is not the placeholder.
func TestPlaceholderNumberInTypedFormats(t *testing.T) {
	docs, err := gaby.ParseAll([]byte("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: app\ndata:\n  port: \"999999999\"\n  replicas: 999999999\n"))
	require.NoError(t, err)
	_, output, err := genericFnGetPlaceholders(testResourceProvider, nil, docs, nil, &api.FunctionOptions{})
	require.NoError(t, err)
	found, ok := output.(api.AttributeValueList)
	require.True(t, ok)
	require.Len(t, found, 1)
	assert.Equal(t, "data.replicas", string(found[0].Path))
}
