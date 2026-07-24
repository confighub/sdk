// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"strings"
	"testing"

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
