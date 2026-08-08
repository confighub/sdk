// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package yamlkit

import (
	"testing"
)

func TestCommentKey(t *testing.T) {
	tests := []struct {
		ct       CommentType
		target   string
		expected string
	}{
		{CommentHead, "server", "$comment$head$server"},
		{CommentLine, "port", "$comment$line$port"},
		{CommentFoot, "database", "$comment$foot$database"},
		{CommentHead, "", "$comment$head$"},
		{CommentFoot, "", "$comment$foot$"},
		{CommentLine, "key:with:colons", "$comment$line$key:with:colons"},
	}
	for _, tt := range tests {
		got := CommentKey(tt.ct, tt.target)
		if got != tt.expected {
			t.Errorf("CommentKey(%q, %q) = %q, want %q", tt.ct, tt.target, got, tt.expected)
		}
	}
}

func TestParseCommentKey(t *testing.T) {
	tests := []struct {
		key        string
		wantType   CommentType
		wantTarget string
		wantOk     bool
	}{
		{"$comment$head$server", CommentHead, "server", true},
		{"$comment$line$port", CommentLine, "port", true},
		{"$comment$foot$database", CommentFoot, "database", true},
		{"$comment$head$", CommentHead, "", true},
		{"$comment$foot$", CommentFoot, "", true},
		{"$comment$line$key:with:colons", CommentLine, "key:with:colons", true},
		// Not comment keys
		{"server", "", "", false},
		{"$visitor", "", "", false},
		{"$comment$", "", "", false},        // no type
		{"$comment$bad$key", "", "", false}, // invalid type
		{"notacomment:head:foo", "", "", false},
	}
	for _, tt := range tests {
		ct, target, ok := ParseCommentKey(tt.key)
		if ok != tt.wantOk || ct != tt.wantType || target != tt.wantTarget {
			t.Errorf("ParseCommentKey(%q) = (%q, %q, %v), want (%q, %q, %v)",
				tt.key, ct, target, ok, tt.wantType, tt.wantTarget, tt.wantOk)
		}
	}
}

func TestIsCommentKey(t *testing.T) {
	if !IsCommentKey("$comment$head$foo") {
		t.Error("expected $comment$head$foo to be a comment key")
	}
	if IsCommentKey("foo") {
		t.Error("expected foo to not be a comment key")
	}
	if IsCommentKey("$comment$bad$foo") {
		t.Error("expected $comment$bad$foo to not be a comment key")
	}
}

func TestRoundTripCommentKey(t *testing.T) {
	for _, ct := range []CommentType{CommentHead, CommentLine, CommentFoot} {
		for _, target := range []string{"foo", "", "a:b", "long/path"} {
			key := CommentKey(ct, target)
			gotType, gotTarget, ok := ParseCommentKey(key)
			if !ok {
				t.Errorf("ParseCommentKey(CommentKey(%q, %q)) returned ok=false", ct, target)
				continue
			}
			if gotType != ct || gotTarget != target {
				t.Errorf("round-trip failed: CommentKey(%q, %q) -> ParseCommentKey -> (%q, %q)",
					ct, target, gotType, gotTarget)
			}
		}
	}
}

func TestStripCommentKeys(t *testing.T) {
	input := map[string]any{
		"$comment$head$server": "Primary server",
		"server":               "192.168.1.1",
		"$comment$line$port":   "default port",
		"port":                 5432,
		"nested": map[string]any{
			"$comment$head$enabled": "SSL flag",
			"enabled":               true,
		},
		"list": []any{
			map[string]any{
				"$comment$head$": "first entry",
				"name":           "alpha",
			},
			"plain-string",
		},
	}

	result := StripCommentKeys(input)
	resultMap := result.(map[string]any)

	// Top level: no comment keys
	if _, ok := resultMap["$comment$head$server"]; ok {
		t.Error("expected $comment$head$server to be stripped")
	}
	if _, ok := resultMap["$comment$line$port"]; ok {
		t.Error("expected $comment$line$port to be stripped")
	}
	if resultMap["server"] != "192.168.1.1" {
		t.Error("expected server to be preserved")
	}
	if resultMap["port"] != 5432 {
		t.Error("expected port to be preserved")
	}

	// Nested map
	nested := resultMap["nested"].(map[string]any)
	if _, ok := nested["$comment$head$enabled"]; ok {
		t.Error("expected nested comment key to be stripped")
	}
	if nested["enabled"] != true {
		t.Error("expected nested enabled to be preserved")
	}

	// Array
	list := resultMap["list"].([]any)
	listEntry := list[0].(map[string]any)
	if _, ok := listEntry["$comment$head$"]; ok {
		t.Error("expected array element comment key to be stripped")
	}
	if listEntry["name"] != "alpha" {
		t.Error("expected array element name to be preserved")
	}
	if list[1] != "plain-string" {
		t.Error("expected plain string in array to be preserved")
	}

	// Original should be unmodified (StripCommentKeys returns a deep copy)
	if _, ok := input["$comment$head$server"]; !ok {
		t.Error("original should not be modified")
	}
}

func TestExtractCommentKeys(t *testing.T) {
	data := map[string]any{
		"$comment$head$server": "Primary server",
		"server":               "192.168.1.1",
		"$comment$line$port":   "default port",
		"port":                 5432,
	}

	comments := ExtractCommentKeys(data)

	if len(comments) != 2 {
		t.Fatalf("expected 2 comments, got %d", len(comments))
	}
	if comments["$comment$head$server"] != "Primary server" {
		t.Error("expected head comment for server")
	}
	if comments["$comment$line$port"] != "default port" {
		t.Error("expected line comment for port")
	}

	// Data should have comment keys removed
	if _, ok := data["$comment$head$server"]; ok {
		t.Error("expected comment key to be removed from data")
	}
	if data["server"] != "192.168.1.1" {
		t.Error("expected data key to be preserved")
	}
}

func TestInjectCommentKeys(t *testing.T) {
	data := map[string]any{
		"server": "192.168.1.1",
	}
	comments := map[string]string{
		"$comment$head$server": "Primary server",
	}

	InjectCommentKeys(data, comments)

	if data["$comment$head$server"] != "Primary server" {
		t.Error("expected comment key to be injected")
	}
	if data["server"] != "192.168.1.1" {
		t.Error("expected existing key to be preserved")
	}
}
