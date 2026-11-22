// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package generic

import (
	"encoding/json"
	"testing"

	"github.com/confighub/sdk/configkit/envkit"
	"github.com/confighub/sdk/configkit/propkit"
	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/third_party/gaby"
	"github.com/confighub/sdk/core/workerapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func decodeJSON(t *testing.T, text string) any {
	t.Helper()
	var v any
	require.NoError(t, json.Unmarshal([]byte(text), &v))
	return v
}

func TestCoerceTextToSchemaTypes(t *testing.T) {
	schema := decodeJSON(t, `{
		"type": "object",
		"$defs": {
			"port": {"type": "integer"},
			"node": {"type": "object", "properties": {"weight": {"type": "number"}, "child": {"$ref": "#/$defs/node"}}}
		},
		"properties": {
			"port": {"$ref": "#/$defs/port"},
			"ratio": {"type": "number"},
			"enabled": {"type": "boolean"},
			"name": {"type": "string"},
			"either": {"type": ["integer", "string"]},
			"combined": {"allOf": [{"minimum": 1}, {"type": "integer"}]},
			"alternative": {"anyOf": [{"type": "boolean"}, {"type": "null"}]},
			"list": {"type": "array", "items": {"type": "integer"}},
			"tuple": {"type": "array", "items": [{"type": "boolean"}], "additionalItems": {"type": "integer"}},
			"tree": {"$ref": "#/$defs/node"},
			"wholeFloat": {"type": "integer"},
			"big": {"type": "integer"},
			"fraction": {"type": "integer"},
			"plus": {"type": "integer"}
		},
		"patternProperties": {"^limit_": {"type": "integer"}},
		"additionalProperties": {"type": "boolean"}
	}`)
	doc := decodeJSON(t, `{
		"port": "8080",
		"ratio": "1.10",
		"enabled": "true",
		"name": "8080",
		"either": "8080",
		"combined": "3",
		"alternative": "false",
		"list": ["1", "x", "3"],
		"tuple": ["true", "2"],
		"tree": {"weight": "0.5", "child": {"weight": "2", "child": {"weight": "heavy"}}},
		"limit_cpu": "4",
		"other": "false",
		"notInt": "1.5",
		"version": "1.10",
		"wholeFloat": "1.0",
		"big": "12345678901234567890",
		"fraction": "2.5",
		"plus": "+5"
	}`)
	got, err := json.Marshal(CoerceTextToSchemaTypes(doc, schema))
	require.NoError(t, err)
	assert.JSONEq(t, `{
		"port": 8080,
		"ratio": 1.10,
		"enabled": true,
		"name": "8080",
		"either": "8080",
		"combined": 3,
		"alternative": false,
		"list": [1, "x", 3],
		"tuple": [true, 2],
		"tree": {"weight": 0.5, "child": {"weight": 2, "child": {"weight": "heavy"}}},
		"limit_cpu": 4,
		"other": false,
		"notInt": "1.5",
		"version": "1.10",
		"wholeFloat": 1.0,
		"big": 12345678901234567890,
		"fraction": 2.5,
		"plus": 5
	}`, string(got))
	// The number keeps its text.
	assert.Contains(t, string(got), `"ratio":1.10`)
}

const portSchema = `{"type": "object", "properties": {"server": {"type": "object", "properties": {
	"port": {"type": "integer", "minimum": 1, "maximum": 65535},
	"debug": {"type": "boolean"}}}}}`

func vetProperties(t *testing.T, properties string) api.ValidationResult {
	t.Helper()
	rp := propkit.NewPropertiesResourceProvider()
	y, err := rp.NativeToYAML([]byte(properties))
	require.NoError(t, err)
	docs, err := gaby.ParseAll(y)
	require.NoError(t, err)
	context := &api.FunctionContext{ToolchainType: workerapi.ToolchainAppConfigProperties}
	_, output, err := GenericFnVetJSONSchema(rp, &api.FunctionOptions{}, context, docs,
		[]api.FunctionArgument{{Value: `{"NoSchema": ` + portSchema + `}`}})
	require.NoError(t, err)
	result, ok := output.(api.ValidationResult)
	require.True(t, ok)
	return result
}

// A property is text, so a schema's integer or boolean is checked against what the text is,
// including the schema's other keywords.
func TestVetJSONSchemaCoercesTextFormats(t *testing.T) {
	assert.True(t, vetProperties(t, "server.port=8080\nserver.debug=true\n").Passed)
	assert.True(t, vetProperties(t, "server.port=8080.0\n").Passed, "a whole number written with a fraction is an integer")

	for _, properties := range []string{
		"server.port=70000\n",
		"server.port=eighty\n",
		"server.port=80.5\n",
		"server.debug=yes\n",
	} {
		result := vetProperties(t, properties)
		assert.False(t, result.Passed, "%q passed", properties)
	}
}

// YAML has types, so a string in a format that isn't text stays a string.
func TestVetJSONSchemaDoesNotCoerceTypedFormats(t *testing.T) {
	docs, err := gaby.ParseAll([]byte("apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: app\nspec:\n  replicas: \"3\"\n"))
	require.NoError(t, err)
	_, output, err := GenericFnVetJSONSchema(testResourceProvider, &api.FunctionOptions{}, nil, docs,
		[]api.FunctionArgument{{Value: `{"apps/v1/Deployment": {"properties": {"spec": {"properties": {"replicas": {"type": "integer"}}}}}}`}})
	require.NoError(t, err)
	result, ok := output.(api.ValidationResult)
	require.True(t, ok)
	assert.False(t, result.Passed, "a YAML string passed as an integer")
}

func TestVetJSONSchemaFromUnitCoercesTextFormats(t *testing.T) {
	rp := envkit.NewEnvResourceProvider()
	schema, err := gaby.ParseAll([]byte("type: object\nproperties:\n  PORT:\n    type: integer\n    maximum: 65535\n  DEBUG:\n    type: boolean\n"))
	require.NoError(t, err)
	for env, want := range map[string]bool{
		"PORT=8080\nDEBUG=false\n": true,
		"PORT=70000\n":             false,
		"PORT=eighty\n":            false,
		"DEBUG=1\n":                false,
	} {
		y, err := rp.NativeToYAML([]byte(env))
		require.NoError(t, err)
		docs, err := gaby.ParseAll(y)
		require.NoError(t, err)
		_, output, err := GenericFnVetJSONSchemaFromUnit(rp, &api.FunctionOptions{}, nil, docs, schema, false)
		require.NoError(t, err)
		result, ok := output.(api.ValidationResult)
		require.True(t, ok)
		assert.Equal(t, want, result.Passed, "%q: %v", env, result.Details)
	}
}

// Env values are text, so the functions that get and set ints and bools work on them.
func TestEnvIntAndBoolFunctions(t *testing.T) {
	rp := envkit.NewEnvResourceProvider()
	y, err := rp.NativeToYAML([]byte("PORT=5432\nDEBUG=true\n"))
	require.NoError(t, err)
	docs, err := gaby.ParseAll(y)
	require.NoError(t, err)
	paths := func(path string) api.ResourceTypeToPathToVisitorInfoType {
		return yamlkit.GetVisitorMapForPath(rp, api.ResourceTypeAny, api.UnresolvedPath(path))
	}
	ports, err := yamlkit.GetPaths[int](docs, paths("PORT"), nil, rp, nil)
	require.NoError(t, err)
	require.Len(t, ports, 1)
	assert.Equal(t, 5432, ports[0].Value)
	require.NoError(t, yamlkit.UpdatePathsValue[bool](docs, paths("DEBUG"), nil, rp, false, false, nil))
	out, err := rp.YAMLToNative([]byte(docs[0].String()))
	require.NoError(t, err)
	assert.Equal(t, "PORT=5432\nDEBUG=false\n", string(out))
}
