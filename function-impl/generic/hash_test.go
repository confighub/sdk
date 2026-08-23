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
	"github.com/confighub/sdk/core/function/handler"
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

// signatureCapture records what a register* function declares, so a test can assert on a
// signature without standing up an executor. Only RegisterFunction is ever called; the
// embedded interface supplies the rest of the method set.
type signatureCapture struct {
	handler.FunctionRegistry
	signatures map[string]api.FunctionSignature
}

func (c *signatureCapture) RegisterFunction(name string, reg *handler.FunctionRegistration) error {
	c.signatures[name] = reg.FunctionSignature
	return nil
}

// get-hash exists to be an UpstreamGetter on a TransformPaths Link, and
// validateLinkTransformPathsFunctions refuses to create such a Link unless the function is
// non-mutating and returns an AttributeValueList. Declaring either differently would leave
// the function callable but the Link uncreatable.
func TestGetHash_SignatureIsUsableAsUpstreamGetter(t *testing.T) {
	rp := k8skit.NewK8sResourceProvider()
	capture := &signatureCapture{signatures: map[string]api.FunctionSignature{}}

	registerGetHash(capture, rp, rp)

	sig, ok := capture.signatures["get-hash"]
	require.True(t, ok, "get-hash should be registered")

	assert.False(t, sig.Mutating, "an UpstreamGetter must be non-mutating")
	require.NotNil(t, sig.OutputInfo, "an UpstreamGetter must declare its output")
	assert.Equal(t, api.OutputTypeAttributeValueList, sig.OutputInfo.OutputType,
		"an UpstreamGetter must return an AttributeValueList")
}

func TestGetHash_ConfigMapData(t *testing.T) {
	rp := k8skit.NewK8sResourceProvider()
	parsedData, err := gaby.ParseAll([]byte(configMapYAML))
	require.NoError(t, err)

	args := []api.FunctionArgument{
		{Value: "data"},
	}

	_, output, err := GenericFnGetHash(rp, parsedData, args, nil)
	require.NoError(t, err)

	values, ok := output.(api.AttributeValueList)
	require.True(t, ok, "get-hash should return an AttributeValueList")
	require.Len(t, values, 1)

	hashValue, ok := values[0].Value.(string)
	require.True(t, ok, "hash value should be a string")
	assert.Len(t, hashValue, 10, "hash should be truncated to 10 hex characters")
	_, err = hex.DecodeString(hashValue)
	require.NoError(t, err, "hash should be valid hex")

	assert.Equal(t, api.DataTypeString, values[0].DataType)
	assert.Equal(t, api.ResourceName("default/my-config"), values[0].ResourceName, "the resource the hash was computed from")
	assert.Equal(t, api.ResolvedPath("data"), values[0].Path, "the path that was hashed")
}

// The reason both functions exist: what a Link propagates has to be the same value a Unit
// would have stored, or the two ways of getting a content hash would disagree.
func TestGetHash_MatchesSetHash(t *testing.T) {
	rp := k8skit.NewK8sResourceProvider()

	setData, err := gaby.ParseAll([]byte(configMapYAML))
	require.NoError(t, err)
	getData, err := gaby.ParseAll([]byte(configMapYAML))
	require.NoError(t, err)

	args := []api.FunctionArgument{
		{Value: "data"},
	}

	setResult, _, err := GenericFnSetHash(rp, setData, args, nil)
	require.NoError(t, err)
	storedHash, found, err := yamlkit.YamlSafePathGetValue[string](setResult[0], api.ResolvedPath(rp.ContextPath("Hash")), true)
	require.NoError(t, err)
	require.True(t, found)

	_, output, err := GenericFnGetHash(rp, getData, args, nil)
	require.NoError(t, err)
	values := output.(api.AttributeValueList)
	require.Len(t, values, 1)

	assert.Equal(t, storedHash, values[0].Value, "get-hash must return the hash set-hash stores")
}

func TestGetHash_DoesNotModifyData(t *testing.T) {
	rp := k8skit.NewK8sResourceProvider()
	parsedData, err := gaby.ParseAll([]byte(configMapYAML))
	require.NoError(t, err)

	before := parsedData[0].String()

	args := []api.FunctionArgument{
		{Value: "data"},
	}

	result, _, err := GenericFnGetHash(rp, parsedData, args, nil)
	require.NoError(t, err)
	require.Len(t, result, 1)

	assert.Equal(t, before, result[0].String(), "get-hash must leave the configuration data alone")

	_, found, _ := yamlkit.YamlSafePathGetValue[string](result[0], api.ResolvedPath(rp.ContextPath("Hash")), true)
	assert.False(t, found, "get-hash must not store the hash it computed")
}

func TestGetHash_MultipleResources(t *testing.T) {
	rp := k8skit.NewK8sResourceProvider()
	parsedData, err := gaby.ParseAll([]byte(twoConfigMapsYAML))
	require.NoError(t, err)

	args := []api.FunctionArgument{
		{Value: "data"},
	}

	_, output, err := GenericFnGetHash(rp, parsedData, args, nil)
	require.NoError(t, err)

	values := output.(api.AttributeValueList)
	require.Len(t, values, 2, "one entry per resource")

	assert.Equal(t, api.ResourceName("default/config-a"), values[0].ResourceName)
	assert.Equal(t, api.ResourceName("default/config-b"), values[1].ResourceName)
	assert.NotEqual(t, values[0].Value, values[1].Value, "different data should produce different hashes")
}

func TestGetHash_NoMatchingPath(t *testing.T) {
	rp := k8skit.NewK8sResourceProvider()
	parsedData, err := gaby.ParseAll([]byte(configMapYAML))
	require.NoError(t, err)

	args := []api.FunctionArgument{
		{Value: "nonexistent"},
	}

	_, output, err := GenericFnGetHash(rp, parsedData, args, nil)
	require.NoError(t, err)

	values := output.(api.AttributeValueList)
	assert.Empty(t, values, "a resource with no values at the path contributes no hash")
}

func TestGetHash_DifferentDataProducesDifferentHash(t *testing.T) {
	rp := k8skit.NewK8sResourceProvider()

	parsedData1, err := gaby.ParseAll([]byte(configMapYAML))
	require.NoError(t, err)
	parsedData2, err := gaby.ParseAll([]byte(configMapDifferentData))
	require.NoError(t, err)

	args := []api.FunctionArgument{
		{Value: "data"},
	}

	_, output1, err := GenericFnGetHash(rp, parsedData1, args, nil)
	require.NoError(t, err)
	_, output2, err := GenericFnGetHash(rp, parsedData2, args, nil)
	require.NoError(t, err)

	assert.NotEqual(t,
		output1.(api.AttributeValueList)[0].Value,
		output2.(api.AttributeValueList)[0].Value,
		"different data should produce different hashes")
}

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
