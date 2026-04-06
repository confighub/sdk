// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package generic

import (
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/confighub/sdk/configkit/k8skit"
	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/third_party/gaby"
)

const configMapYAML = `apiVersion: v1
kind: ConfigMap
metadata:
  name: my-config
  namespace: default
data:
  key1: value1
  key2: value2
`

func TestSetHash_ConfigMapData(t *testing.T) {
	rp := k8skit.NewK8sResourceProvider()
	parsedData, err := gaby.ParseAll([]byte(configMapYAML))
	require.NoError(t, err)

	args := []api.FunctionArgument{
		{Value: "data"},
	}

	result, _, err := GenericFnSetHash(rp, parsedData, args, nil)
	require.NoError(t, err)
	require.Len(t, result, 1)

	// Verify the hash annotation was set
	hashAnnotationPath := rp.ContextPath("Hash")
	hashDoc := result[0]
	hashValue, found, err := yamlkit.YamlSafePathGetValue[string](hashDoc, api.ResolvedPath(hashAnnotationPath), true)
	require.NoError(t, err)
	require.True(t, found, "confighub.com/Hash annotation should be present")

	// Verify it's a truncated sha256 hex string (10 hex chars, matching configmap bridge)
	assert.Len(t, hashValue, 10, "hash should be truncated to 10 hex characters")
	_, err = hex.DecodeString(hashValue)
	require.NoError(t, err, "hash should be valid hex")
}

const twoConfigMapsYAML = `apiVersion: v1
kind: ConfigMap
metadata:
  name: config-a
  namespace: default
data:
  foo: bar
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: config-b
  namespace: default
data:
  baz: qux
`

func TestSetHash_MultipleResources(t *testing.T) {
	rp := k8skit.NewK8sResourceProvider()
	parsedData, err := gaby.ParseAll([]byte(twoConfigMapsYAML))
	require.NoError(t, err)

	args := []api.FunctionArgument{
		{Value: "data"},
	}

	result, _, err := GenericFnSetHash(rp, parsedData, args, nil)
	require.NoError(t, err)
	require.Len(t, result, 2)

	hashAnnotationPath := rp.ContextPath("Hash")

	// Both docs should have hash annotations
	for i, doc := range result {
		hashValue, found, err := yamlkit.YamlSafePathGetValue[string](doc, api.ResolvedPath(hashAnnotationPath), true)
		require.NoError(t, err)
		require.True(t, found, "hash annotation should be present on doc %d", i)
		assert.NotEmpty(t, hashValue, "hash should not be empty on doc %d", i)
	}

	// Hashes should differ since the data differs
	hash0, _, _ := yamlkit.YamlSafePathGetValue[string](result[0], api.ResolvedPath(hashAnnotationPath), true)
	hash1, _, _ := yamlkit.YamlSafePathGetValue[string](result[1], api.ResolvedPath(hashAnnotationPath), true)
	assert.NotEqual(t, hash0, hash1, "different data should produce different hashes")
}

func TestSetHash_NoMatchingPath(t *testing.T) {
	rp := k8skit.NewK8sResourceProvider()
	parsedData, err := gaby.ParseAll([]byte(configMapYAML))
	require.NoError(t, err)

	args := []api.FunctionArgument{
		{Value: "nonexistent"},
	}

	result, _, err := GenericFnSetHash(rp, parsedData, args, nil)
	require.NoError(t, err)

	// No hash should be set since the path doesn't exist
	hashAnnotationPath := rp.ContextPath("Hash")
	_, found, _ := yamlkit.YamlSafePathGetValue[string](result[0], api.ResolvedPath(hashAnnotationPath), true)
	assert.False(t, found, "hash annotation should not be present when path doesn't match")
}

func TestSetHash_Idempotent(t *testing.T) {
	rp := k8skit.NewK8sResourceProvider()
	parsedData, err := gaby.ParseAll([]byte(configMapYAML))
	require.NoError(t, err)

	args := []api.FunctionArgument{
		{Value: "data"},
	}

	// Run twice
	result1, _, err := GenericFnSetHash(rp, parsedData, args, nil)
	require.NoError(t, err)
	result2, _, err := GenericFnSetHash(rp, result1, args, nil)
	require.NoError(t, err)

	hashAnnotationPath := rp.ContextPath("Hash")
	hash1, _, _ := yamlkit.YamlSafePathGetValue[string](result1[0], api.ResolvedPath(hashAnnotationPath), true)
	hash2, _, _ := yamlkit.YamlSafePathGetValue[string](result2[0], api.ResolvedPath(hashAnnotationPath), true)
	assert.Equal(t, hash1, hash2, "set-hash should be idempotent")
}

const configMapDifferentData = `apiVersion: v1
kind: ConfigMap
metadata:
  name: my-config
  namespace: default
data:
  key1: changed
  key2: value2
`

func TestSetHash_DifferentDataProducesDifferentHash(t *testing.T) {
	rp := k8skit.NewK8sResourceProvider()

	parsedData1, err := gaby.ParseAll([]byte(configMapYAML))
	require.NoError(t, err)
	parsedData2, err := gaby.ParseAll([]byte(configMapDifferentData))
	require.NoError(t, err)

	args := []api.FunctionArgument{
		{Value: "data"},
	}

	result1, _, err := GenericFnSetHash(rp, parsedData1, args, nil)
	require.NoError(t, err)
	result2, _, err := GenericFnSetHash(rp, parsedData2, args, nil)
	require.NoError(t, err)

	hashAnnotationPath := rp.ContextPath("Hash")
	hash1, _, _ := yamlkit.YamlSafePathGetValue[string](result1[0], api.ResolvedPath(hashAnnotationPath), true)
	hash2, _, _ := yamlkit.YamlSafePathGetValue[string](result2[0], api.ResolvedPath(hashAnnotationPath), true)
	assert.NotEqual(t, hash1, hash2, "different data should produce different hashes")
}
