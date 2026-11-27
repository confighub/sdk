// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package kubernetes

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/third_party/gaby"
	"github.com/confighub/sdk/function-impl/generic"
)

// These run set-hash and get-hash against the Kubernetes resource provider with its attribute
// paths registered, which is what says data and binaryData are the content a ConfigMap's hash
// is computed over.

const emptyConfigMapYAML = `apiVersion: v1
kind: ConfigMap
metadata:
  name: empty-config
  namespace: default
`

// emptyHash is the hash of no values: the first ten hex characters of the SHA-256 of nothing.
const emptyHash = "e3b0c44298"

// A ConfigMap without data is still used by pod templates, whose hash annotations are filled
// from its hash, so it has one.
func TestSetHash_ConfigMapWithoutData(t *testing.T) {
	rp := testResourceProvider
	parsedData, err := gaby.ParseAll([]byte(emptyConfigMapYAML))
	require.NoError(t, err)

	args := []api.FunctionArgument{{Value: "data"}, {Value: "binaryData"}}
	result, _, err := generic.GenericFnSetHash(rp, parsedData, args, nil)
	require.NoError(t, err)

	hashValue, found, err := yamlkit.YamlSafePathGetValue[string](result[0], api.ResolvedPath(rp.ContextPath("Hash")), true)
	require.NoError(t, err)
	require.True(t, found, "a ConfigMap without data should get a hash")
	assert.Equal(t, emptyHash, hashValue)
}

func TestGetHash_ConfigMapWithoutData(t *testing.T) {
	rp := testResourceProvider
	parsedData, err := gaby.ParseAll([]byte(emptyConfigMapYAML))
	require.NoError(t, err)

	args := []api.FunctionArgument{{Value: "data"}, {Value: "binaryData"}}
	_, output, err := generic.GenericFnGetHash(rp, parsedData, args, nil)
	require.NoError(t, err)

	values := output.(api.AttributeValueList)
	require.Len(t, values, 1)
	assert.Equal(t, emptyHash, values[0].Value)
}

const deploymentWithoutDataYAML = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
  namespace: default
spec:
  replicas: 1
`

// Only a type that declares the paths as its hashed content has a hash without values there:
// other resources are left alone.
func TestHash_OtherTypeWithoutValues(t *testing.T) {
	rp := testResourceProvider
	args := []api.FunctionArgument{{Value: "data"}, {Value: "binaryData"}}

	parsedData, err := gaby.ParseAll([]byte(deploymentWithoutDataYAML))
	require.NoError(t, err)
	result, _, err := generic.GenericFnSetHash(rp, parsedData, args, nil)
	require.NoError(t, err)
	_, found, _ := yamlkit.YamlSafePathGetValue[string](result[0], api.ResolvedPath(rp.ContextPath("Hash")), true)
	assert.False(t, found, "a Deployment should get no hash from paths it lacks")

	_, output, err := generic.GenericFnGetHash(rp, parsedData, args, nil)
	require.NoError(t, err)
	assert.Empty(t, output.(api.AttributeValueList))
}

const deploymentAndConfigMapYAML = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
  namespace: default
spec:
  replicas: 1
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: my-config
  namespace: default
  annotations:
    confighub.com/Hash: abcdef1234
data:
  key: value
`

// Hashing a path that is not a ConfigMap's content leaves the ConfigMap, and the hash it has,
// alone.
func TestHash_OtherPathLeavesConfigMapAlone(t *testing.T) {
	rp := testResourceProvider
	args := []api.FunctionArgument{{Value: "spec.replicas"}}

	parsedData, err := gaby.ParseAll([]byte(deploymentAndConfigMapYAML))
	require.NoError(t, err)
	result, _, err := generic.GenericFnSetHash(rp, parsedData, args, nil)
	require.NoError(t, err)
	require.Len(t, result, 2)
	hashValue, found, err := yamlkit.YamlSafePathGetValue[string](result[1], api.ResolvedPath(rp.ContextPath("Hash")), true)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "abcdef1234", hashValue, "the ConfigMap's hash should be left as it was")

	_, output, err := generic.GenericFnGetHash(rp, parsedData, args, nil)
	require.NoError(t, err)
	values := output.(api.AttributeValueList)
	require.Len(t, values, 1, "only the Deployment has a value at the path")
	assert.Equal(t, api.ResourceType("apps/v1/Deployment"), values[0].ResourceType)
}

const binaryOnlyConfigMapYAML = `apiVersion: v1
kind: ConfigMap
metadata:
  name: binary-config
  namespace: default
binaryData:
  blob: aGVsbG8=
`

// A ConfigMap holding only binaryData has no hash of its data alone: the hash of no values
// would stay the same through every change to binaryData. A ConfigMap with neither has it
// whichever of the two paths is hashed.
func TestHash_DataOnlyOnConfigMapWithBinaryData(t *testing.T) {
	rp := testResourceProvider
	args := []api.FunctionArgument{{Value: "data"}}

	parsedData, err := gaby.ParseAll([]byte(binaryOnlyConfigMapYAML))
	require.NoError(t, err)
	result, _, err := generic.GenericFnSetHash(rp, parsedData, args, nil)
	require.NoError(t, err)
	_, found, _ := yamlkit.YamlSafePathGetValue[string](result[0], api.ResolvedPath(rp.ContextPath("Hash")), true)
	assert.False(t, found, "a ConfigMap with binaryData should get no hash from data alone")

	_, output, err := generic.GenericFnGetHash(rp, parsedData, args, nil)
	require.NoError(t, err)
	assert.Empty(t, output.(api.AttributeValueList))

	parsedData, err = gaby.ParseAll([]byte(emptyConfigMapYAML))
	require.NoError(t, err)
	_, output, err = generic.GenericFnGetHash(rp, parsedData, args, nil)
	require.NoError(t, err)
	values := output.(api.AttributeValueList)
	require.Len(t, values, 1)
	assert.Equal(t, emptyHash, values[0].Value)
}
