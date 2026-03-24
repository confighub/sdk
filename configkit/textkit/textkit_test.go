// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package textkit

import (
	"testing"
)

func TestNativeToYAML(t *testing.T) {
	rp := NewTextResourceProvider()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "empty input",
			input:    "",
			expected: "",
		},
		{
			name:  "single line",
			input: "hello world\n",
			expected: `text: |
  hello world
`,
		},
		{
			name:  "multiple lines",
			input: "line one\nline two\nline three\n",
			expected: `text: |
  line one
  line two
  line three
`,
		},
		{
			name:  "lines with special characters",
			input: "# Header\n\nkey: value\n",
			expected: `text: |
  # Header

  key: value
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := rp.NativeToYAML([]byte(tt.input))
			if err != nil {
				t.Fatalf("NativeToYAML() error = %v", err)
			}
			if string(result) != tt.expected {
				t.Errorf("NativeToYAML() =\n%s\nwant:\n%s", string(result), tt.expected)
			}
		})
	}
}

func TestYAMLToNative(t *testing.T) {
	rp := NewTextResourceProvider()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "empty input",
			input:    "",
			expected: "",
		},
		{
			name: "single line",
			input: `text: |
  hello world
`,
			expected: "hello world\n",
		},
		{
			name: "multiple lines",
			input: `text: |
  line one
  line two
  line three
`,
			expected: "line one\nline two\nline three\n",
		},
		{
			name: "with configHub metadata stripped",
			input: `configHub:
  configName: myconfig
  configSchema: text
text: |
  hello
  world
`,
			expected: "hello\nworld\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := rp.YAMLToNative([]byte(tt.input))
			if err != nil {
				t.Fatalf("YAMLToNative() error = %v", err)
			}
			if string(result) != tt.expected {
				t.Errorf("YAMLToNative() = %q, want %q", string(result), tt.expected)
			}
		})
	}
}

func TestRoundTrip(t *testing.T) {
	rp := NewTextResourceProvider()

	input := "# My Document\n\nSome content here.\nMore content.\n"

	yamlData, err := rp.NativeToYAML([]byte(input))
	if err != nil {
		t.Fatalf("NativeToYAML() error = %v", err)
	}

	result, err := rp.YAMLToNative(yamlData)
	if err != nil {
		t.Fatalf("YAMLToNative() error = %v", err)
	}

	if string(result) != input {
		t.Errorf("Round trip failed:\ngot:  %q\nwant: %q", string(result), input)
	}
}
