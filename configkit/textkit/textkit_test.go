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
		{
			name:  "with frontmatter",
			input: "---\nconfigHub:\n  configName: myconfig\n  configSchema: text\n---\nhello\nworld\n",
			expected: `frontMatter:
  configHub:
    configName: myconfig
    configSchema: text
text: |
  hello
  world
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
			name: "with frontMatter metadata produces frontmatter",
			input: `frontMatter:
  configHub:
    configName: myconfig
    configSchema: text
text: |
  hello
  world
`,
			expected: "---\nconfigHub:\n  configName: myconfig\n  configSchema: text\n---\nhello\nworld\n",
		},
		{
			name: "without frontMatter metadata no frontmatter",
			input: `text: |
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

func TestRoundTripWithFrontMatter(t *testing.T) {
	rp := NewTextResourceProvider()

	input := "---\nconfigHub:\n  configName: myconfig\n  configSchema: text\n---\n# My Document\n\nSome content here.\nMore content.\n"

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

func TestParseFrontMatter(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		hasFM      bool
		fmKeys     []string
		bodyPrefix string
	}{
		{
			name:       "no frontmatter",
			input:      "just some text\n",
			hasFM:      false,
			bodyPrefix: "just some text",
		},
		{
			name:       "frontmatter present",
			input:      "---\nkey: value\n---\nbody text\n",
			hasFM:      true,
			fmKeys:     []string{"key"},
			bodyPrefix: "body text",
		},
		{
			name:       "no closing separator",
			input:      "---\nkey: value\nbody text\n",
			hasFM:      false,
			bodyPrefix: "---",
		},
		{
			name:       "not starting with separator",
			input:      "text\n---\nkey: value\n---\n",
			hasFM:      false,
			bodyPrefix: "text",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fm, body, ok := parseFrontMatter(tt.input)
			if ok != tt.hasFM {
				t.Fatalf("parseFrontMatter() ok = %v, want %v", ok, tt.hasFM)
			}
			if tt.hasFM {
				for _, key := range tt.fmKeys {
					if _, exists := fm[key]; !exists {
						t.Errorf("parseFrontMatter() missing key %q", key)
					}
				}
			}
			if len(body) < len(tt.bodyPrefix) || body[:len(tt.bodyPrefix)] != tt.bodyPrefix {
				t.Errorf("parseFrontMatter() body = %q, want prefix %q", body, tt.bodyPrefix)
			}
		})
	}
}
