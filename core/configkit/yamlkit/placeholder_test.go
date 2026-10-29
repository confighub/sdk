// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package yamlkit

import (
	"testing"

	"github.com/confighub/sdk/core/function/api"
)

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
			name:        "named placeholder with digits and underscores",
			input:       "confighubplaceholderapp_env2",
			replacement: "app-env2-7e719321eb",
			expected:    "app-env2-7e719321eb",
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

func TestPlaceholderName(t *testing.T) {
	tests := []struct {
		input string
		name  string
		named bool
	}{
		{"confighubplaceholderapp_env", "app_env", true},
		{"confighubplaceholdersubdomain", "subdomain", true},
		{"confighubplaceholder2", "2", true},
		{"confighubplaceholder", "", false},
		{"confighubplaceholder-http", "", false},
		{"confighubplaceholdersubdomain.example.com", "", false},
		{"prefix-confighubplaceholderapp_env", "", false},
		{"app-env", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			name, named := PlaceholderName(tt.input)
			if name != tt.name || named != tt.named {
				t.Errorf("PlaceholderName(%q) = %q, %v, want %q, %v", tt.input, name, named, tt.name, tt.named)
			}
		})
	}
}

func TestNamedPlaceholderFor(t *testing.T) {
	got := NamedPlaceholderFor("app-env.v1")
	if got != "confighubplaceholderapp_env_v1" {
		t.Errorf("NamedPlaceholderFor(%q) = %q", "app-env.v1", got)
	}
	if name, named := PlaceholderName(got); !named || name != PlaceholderNameFor("app-env.v1") {
		t.Errorf("PlaceholderName(%q) = %q, %v, want the name it was made from", got, name, named)
	}
}

func TestPreferPlaceholderName(t *testing.T) {
	tests := []struct {
		original any
		want     string
	}{
		{"confighubplaceholderapp_env", "app_env"},
		{"confighubplaceholder", ""},
		{"confighubplaceholdersubdomain.example.com", ""},
		{"app-env-f655md8fbd", ""},
		{999999999, ""},
	}
	for _, tt := range tests {
		attr := api.AttributeValue{}
		attr.Details = &api.AttributeDetails{}
		attr.Details.NeededPreferred = map[string]string{"Name": "config"}
		PreferPlaceholderName(&attr, tt.original)
		if got := attr.Details.NeededPreferred[api.PropertyKeyPlaceholderName]; got != tt.want {
			t.Errorf("PreferPlaceholderName(%v) prefers %q, want %q", tt.original, got, tt.want)
		}
		if attr.Details.NeededPreferred["Name"] != "config" {
			t.Errorf("PreferPlaceholderName(%v) dropped the other preferences", tt.original)
		}
	}
}
