// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package yamlkit

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/third_party/gaby"
)

// --- JSON Accessor Tests ---

func TestJSONAccessor_Extract(t *testing.T) {
	ja := &JSONAccessor{}

	tests := []struct {
		name     string
		json     string
		path     string
		expected any
		wantErr  bool
	}{
		{"simple string", `{"name":"alice"}`, "name", "alice", false},
		{"nested field", `{"a":{"b":"hello"}}`, "a.b", "hello", false},
		{"integer value", `{"count":42}`, "count", 42, false},
		{"boolean value", `{"enabled":true}`, "enabled", true, false},
		{"missing path", `{"name":"alice"}`, "missing", nil, true},
		{"invalid json", `not json`, "name", nil, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ja.Extract(tt.json, tt.path)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestJSONAccessor_Replace(t *testing.T) {
	ja := &JSONAccessor{}

	tests := []struct {
		name     string
		json     string
		path     string
		value    any
		contains string
		wantErr  bool
	}{
		{
			"replace string",
			`{"name":"alice"}`,
			"name", "bob",
			`"name":"bob"`,
			false,
		},
		{
			"replace nested",
			`{"a":{"b":"hello"}}`,
			"a.b", "world",
			`"b":"world"`,
			false,
		},
		{
			"replace integer",
			`{"count":42}`,
			"count", 99,
			`"count":99`,
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ja.Replace(tt.json, tt.value, tt.path)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Contains(t, result, tt.contains)
				// Verify round-trip: the replaced value should be extractable
				extracted, err := ja.Extract(result, tt.path)
				require.NoError(t, err)
				assert.Equal(t, tt.value, extracted)
			}
		})
	}
}

func TestJSONAccessor_ExistsP(t *testing.T) {
	ja := &JSONAccessor{}

	yamlStr := "data: '{\"name\":\"alice\",\"age\":30}'\n"
	doc, err := gaby.ParseYAML([]byte(yamlStr))
	require.NoError(t, err)
	dataNode := doc.Path("data")
	require.NotNil(t, dataNode)

	assert.True(t, ja.ExistsP(dataNode, "name"))
	assert.True(t, ja.ExistsP(dataNode, "age"))
	assert.False(t, ja.ExistsP(dataNode, "missing"))
}

func TestJSONAccessor_SetP(t *testing.T) {
	ja := &JSONAccessor{}

	yamlStr := "data: '{\"name\":\"alice\"}'\n"
	doc, err := gaby.ParseYAML([]byte(yamlStr))
	require.NoError(t, err)
	dataNode := doc.Path("data")
	require.NotNil(t, dataNode)

	err = ja.SetP(dataNode, "bob", "name")
	require.NoError(t, err)

	result, ok := dataNode.Data().(string)
	require.True(t, ok)
	assert.Contains(t, result, `"name":"bob"`)
}

func TestJSONAccessor_ViaGetEmbeddedAccessor(t *testing.T) {
	accessor, err := GetEmbeddedAccessor(api.EmbeddedAccessorJSON, "")
	require.NoError(t, err)
	require.NotNil(t, accessor)

	result, err := accessor.Extract(`{"key":"value"}`, "key")
	require.NoError(t, err)
	assert.Equal(t, "value", result)
}

// --- YAML Accessor Tests ---

func TestYAMLAccessor_Extract(t *testing.T) {
	ya := &YAMLAccessor{}

	tests := []struct {
		name     string
		yaml     string
		path     string
		expected any
		wantErr  bool
	}{
		{"simple string", "name: alice\n", "name", "alice", false},
		{"nested field", "a:\n  b: hello\n", "a.b", "hello", false},
		{"integer value", "count: 42\n", "count", 42, false},
		{"boolean value", "enabled: true\n", "enabled", true, false},
		{"missing path", "name: alice\n", "missing", nil, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ya.Extract(tt.yaml, tt.path)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestYAMLAccessor_Replace(t *testing.T) {
	ya := &YAMLAccessor{}

	input := "a:\n  b: hello\n"
	result, err := ya.Replace(input, "world", "a.b")
	require.NoError(t, err)

	// Verify the value was replaced
	extracted, err := ya.Extract(result, "a.b")
	require.NoError(t, err)
	assert.Equal(t, "world", extracted)
}

func TestYAMLAccessor_ExistsP(t *testing.T) {
	ya := &YAMLAccessor{}

	// Embed YAML as a quoted string in a YAML document
	yamlStr := "data: \"name: alice\\nage: 30\\n\"\n"
	doc, err := gaby.ParseYAML([]byte(yamlStr))
	require.NoError(t, err)
	dataNode := doc.Path("data")
	require.NotNil(t, dataNode)

	assert.True(t, ya.ExistsP(dataNode, "name"))
	assert.True(t, ya.ExistsP(dataNode, "age"))
	assert.False(t, ya.ExistsP(dataNode, "missing"))
}

func TestYAMLAccessor_SetP(t *testing.T) {
	ya := &YAMLAccessor{}

	yamlStr := "data: \"name: alice\\n\"\n"
	doc, err := gaby.ParseYAML([]byte(yamlStr))
	require.NoError(t, err)
	dataNode := doc.Path("data")
	require.NotNil(t, dataNode)

	err = ya.SetP(dataNode, "bob", "name")
	require.NoError(t, err)

	result, ok := dataNode.Data().(string)
	require.True(t, ok)
	assert.Contains(t, result, "name: bob")
}

func TestYAMLAccessor_ViaGetEmbeddedAccessor(t *testing.T) {
	accessor, err := GetEmbeddedAccessor(api.EmbeddedAccessorYAML, "")
	require.NoError(t, err)
	require.NotNil(t, accessor)

	result, err := accessor.Extract("key: value\n", "key")
	require.NoError(t, err)
	assert.Equal(t, "value", result)
}
