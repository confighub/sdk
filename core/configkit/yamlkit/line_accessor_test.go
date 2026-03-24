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

func TestLineAccessor_Extract(t *testing.T) {
	la := &LineAccessor{}

	tests := []struct {
		name     string
		input    string
		lineNum  string
		expected string
		wantErr  bool
	}{
		{"first line", "line one\nline two\nline three\n", "1", "line one", false},
		{"second line", "line one\nline two\nline three\n", "2", "line two", false},
		{"last line", "line one\nline two\nline three\n", "3", "line three", false},
		{"out of range", "line one\nline two\n", "5", "", true},
		{"zero", "line one\n", "0", "", true},
		{"negative", "line one\n", "-1", "", true},
		{"not a number", "line one\n", "abc", "", true},
		{"no trailing newline", "line one\nline two", "2", "line two", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := la.Extract(tt.input, tt.lineNum)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestLineAccessor_Replace(t *testing.T) {
	la := &LineAccessor{}

	tests := []struct {
		name     string
		input    string
		lineNum  string
		newValue string
		expected string
		wantErr  bool
	}{
		{
			"replace middle line",
			"line one\nline two\nline three\n",
			"2", "NEW LINE TWO",
			"line one\nNEW LINE TWO\nline three\n",
			false,
		},
		{
			"replace first line",
			"line one\nline two\n",
			"1", "FIRST",
			"FIRST\nline two\n",
			false,
		},
		{
			"replace last line",
			"line one\nline two\nline three\n",
			"3", "THIRD",
			"line one\nline two\nTHIRD\n",
			false,
		},
		{
			"out of range",
			"line one\n",
			"5", "value",
			"line one\n",
			true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := la.Replace(tt.input, tt.newValue, tt.lineNum)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestLineAccessor_ExistsP(t *testing.T) {
	la := &LineAccessor{}

	yamlStr := "text: \"line one\\nline two\\nline three\\n\"\n"
	doc, err := gaby.ParseYAML([]byte(yamlStr))
	require.NoError(t, err)
	textNode := doc.Path("text")
	require.NotNil(t, textNode)

	assert.True(t, la.ExistsP(textNode, "1"))
	assert.True(t, la.ExistsP(textNode, "3"))
	assert.False(t, la.ExistsP(textNode, "4"))
	assert.False(t, la.ExistsP(textNode, "0"))
	assert.False(t, la.ExistsP(textNode, "abc"))
}

func TestLineAccessor_SetP(t *testing.T) {
	la := &LineAccessor{}

	yamlStr := "text: \"line one\\nline two\\nline three\\n\"\n"
	doc, err := gaby.ParseYAML([]byte(yamlStr))
	require.NoError(t, err)
	textNode := doc.Path("text")
	require.NotNil(t, textNode)

	err = la.SetP(textNode, "REPLACED", "2")
	require.NoError(t, err)

	result, ok := textNode.Data().(string)
	require.True(t, ok)
	assert.Equal(t, "line one\nREPLACED\nline three\n", result)
}

func TestLineAccessor_ViaGetEmbeddedAccessor(t *testing.T) {
	// Verify it can be created via the factory
	accessor, err := GetEmbeddedAccessor(api.EmbeddedAccessorLine, "")
	require.NoError(t, err)
	require.NotNil(t, accessor)

	result, err := accessor.Extract("hello\nworld\n", "2")
	require.NoError(t, err)
	assert.Equal(t, "world", result)
}
