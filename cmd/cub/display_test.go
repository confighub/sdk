// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"strings"
	"testing"
	"unicode/utf8"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	sigsyaml "sigs.k8s.io/yaml"
)

// TestMarshalYAMLPreservesFunctionArgumentValue is a regression test for YAML
// output rendering empty FunctionArgument.Value fields. The generated union
// type FunctionArgument_Value keeps its payload in an unexported field and
// implements MarshalJSON but not MarshalYAML, so a plain gopkg.in/yaml.v3
// marshal drops the value. marshalYAML must route through JSON so the value
// survives (matching JSON output).
func TestMarshalYAMLPreservesFunctionArgumentValue(t *testing.T) {
	const celExpr = `has(r.metadata.labels) && "team" in r.metadata.labels`

	var value goclientnew.FunctionArgument_Value
	if err := value.FromFunctionArgumentValue0(celExpr); err != nil {
		t.Fatalf("FromFunctionArgumentValue0: %v", err)
	}
	arg := goclientnew.FunctionArgument{Value: &value}

	out, err := marshalYAML(arg)
	if err != nil {
		t.Fatalf("marshalYAML: %v", err)
	}
	if !strings.Contains(string(out), "team") {
		t.Fatalf("YAML output dropped Value; got:\n%s", out)
	}
}

// TestMarshalYAMLFunctionArgumentValueRoundtrips verifies the value survives a
// full YAML marshal -> unmarshal cycle.
func TestMarshalYAMLFunctionArgumentValueRoundtrips(t *testing.T) {
	cases := map[string]func() goclientnew.FunctionArgument_Value{
		"string": func() goclientnew.FunctionArgument_Value {
			var v goclientnew.FunctionArgument_Value
			if err := v.FromFunctionArgumentValue0("some-string"); err != nil {
				t.Fatalf("FromFunctionArgumentValue0: %v", err)
			}
			return v
		},
		"int": func() goclientnew.FunctionArgument_Value {
			var v goclientnew.FunctionArgument_Value
			if err := v.FromFunctionArgumentValue1(42); err != nil {
				t.Fatalf("FromFunctionArgumentValue1: %v", err)
			}
			return v
		},
		"bool": func() goclientnew.FunctionArgument_Value {
			var v goclientnew.FunctionArgument_Value
			if err := v.FromFunctionArgumentValue2(true); err != nil {
				t.Fatalf("FromFunctionArgumentValue2: %v", err)
			}
			return v
		},
	}

	for name, build := range cases {
		t.Run(name, func(t *testing.T) {
			value := build()
			arg := goclientnew.FunctionArgument{Value: &value}

			out, err := marshalYAML(arg)
			if err != nil {
				t.Fatalf("marshalYAML: %v", err)
			}

			var got goclientnew.FunctionArgument
			if err := sigsyaml.Unmarshal(out, &got); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if got.Value == nil {
				t.Fatalf("roundtrip lost Value; YAML was:\n%s", out)
			}

			wantJSON, err := value.MarshalJSON()
			if err != nil {
				t.Fatalf("MarshalJSON(want): %v", err)
			}
			gotJSON, err := got.Value.MarshalJSON()
			if err != nil {
				t.Fatalf("MarshalJSON(got): %v", err)
			}
			if string(gotJSON) != string(wantJSON) {
				t.Fatalf("roundtrip mismatch: want %s, got %s", wantJSON, gotJSON)
			}
		})
	}
}

// TestFormatFunctionArgumentValue is a regression test for `cub mutation get`
// rendering arguments as Go objects. FunctionArgument_Value keeps its payload
// in an unexported json.RawMessage, so %v prints the raw bytes.
func TestFormatFunctionArgumentValue(t *testing.T) {
	str := func() *goclientnew.FunctionArgument_Value {
		v := &goclientnew.FunctionArgument_Value{}
		if err := v.FromFunctionArgumentValue0("nginx:1.27"); err != nil {
			t.Fatalf("FromFunctionArgumentValue0: %v", err)
		}
		return v
	}
	num := func() *goclientnew.FunctionArgument_Value {
		v := &goclientnew.FunctionArgument_Value{}
		if err := v.FromFunctionArgumentValue1(3); err != nil {
			t.Fatalf("FromFunctionArgumentValue1: %v", err)
		}
		return v
	}
	boolean := func() *goclientnew.FunctionArgument_Value {
		v := &goclientnew.FunctionArgument_Value{}
		if err := v.FromFunctionArgumentValue2(true); err != nil {
			t.Fatalf("FromFunctionArgumentValue2: %v", err)
		}
		return v
	}

	cases := []struct {
		name  string
		value *goclientnew.FunctionArgument_Value
		want  string
	}{
		{"string", str(), `"nginx:1.27"`},
		{"int", num(), "3"},
		{"bool", boolean(), "true"},
		{"nil", nil, "<nil>"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := formatFunctionArgumentValue(tc.value); got != tc.want {
				t.Fatalf("formatFunctionArgumentValue = %s, want %s", got, tc.want)
			}
		})
	}
}

// truncateWithEllipsis is what every list command cuts free text with, so maxLen has to be a
// width a table can be laid out against: the ellipsis counts against it rather than being added
// on top. Cutting UTF-8 by bytes is the other hazard -- a cut inside a rune leaves a byte that
// renders as a replacement character.
func TestTruncateWithEllipsis(t *testing.T) {
	testCases := []struct {
		name   string
		text   string
		maxLen int
		want   string
	}{
		{name: "short enough is left alone", text: "release 42", maxLen: 50, want: "release 42"},
		{name: "exactly the limit is left alone", text: "abcde", maxLen: 5, want: "abcde"},
		{name: "the ellipsis counts against the limit", text: "abcdefghij", maxLen: 5, want: "ab..."},
		{name: "one over", text: "abcdef", maxLen: 5, want: "ab..."},
		{name: "no room for an ellipsis", text: "abcdef", maxLen: 2, want: "ab"},
		{name: "no room at all", text: "abcdef", maxLen: 0, want: "abcdef"},
		{name: "empty", text: "", maxLen: 10, want: ""},
		// The cut lands mid-rune: "é" is two bytes, so a 5-byte budget minus the ellipsis
		// leaves two, and the second is half of it. The partial rune is dropped rather than
		// emitted.
		{name: "a cut inside a rune drops it", text: "aébcdef", maxLen: 5, want: "a..."},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := truncateWithEllipsis(tc.text, tc.maxLen)
			if got != tc.want {
				t.Errorf("truncateWithEllipsis(%q, %d) = %q, want %q", tc.text, tc.maxLen, got, tc.want)
			}
			if tc.maxLen > 0 && len(got) > tc.maxLen {
				t.Errorf("result %q is %d bytes, over the limit of %d", got, len(got), tc.maxLen)
			}
			if !utf8.ValidString(got) {
				t.Errorf("result %q is not valid UTF-8", got)
			}
		})
	}
}
