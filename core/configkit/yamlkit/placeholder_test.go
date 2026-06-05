// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package yamlkit

import "testing"

func TestReplaceStringPlaceholder(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		replacement string
		expected    string
	}{
		{
			name:        "bare placeholder",
			input:       "confighubplaceholder",
			replacement: "myapp",
			expected:    "myapp",
		},
		{
			name:        "named placeholder with surrounding text",
			input:       "confighubplaceholdersubdomain.test.example.com",
			replacement: "myapp",
			expected:    "myapp.test.example.com",
		},
		{
			name:        "placeholder followed by non-alphabetic suffix",
			input:       "confighubplaceholder-http",
			replacement: "myapp",
			expected:    "myapp-http",
		},
		{
			name:        "multiple occurrences",
			input:       "confighubplaceholder-http and confighubplaceholdersubdomain.example.com",
			replacement: "myapp",
			expected:    "myapp-http and myapp.example.com",
		},
		{
			name:        "no placeholder",
			input:       "no-placeholder-here",
			replacement: "myapp",
			expected:    "no-placeholder-here",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ReplaceStringPlaceholder(tt.input, tt.replacement)
			if got != tt.expected {
				t.Errorf("ReplaceStringPlaceholder(%q, %q) = %q, want %q",
					tt.input, tt.replacement, got, tt.expected)
			}
		})
	}
}
