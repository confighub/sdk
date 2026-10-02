// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"reflect"
	"testing"
)

func TestSplitFunctionLine(t *testing.T) {
	tests := []struct {
		line    string
		want    []string
		wantErr bool
	}{
		{line: "set-replicas 3", want: []string{"set-replicas", "3"}},
		{line: "  set-container-image   nginx \t nginx:v234  ", want: []string{"set-container-image", "nginx", "nginx:v234"}},
		{line: "", want: nil},
		{line: "   ", want: nil},
		// An argument holding whitespace is quoted, with either quote.
		{line: `vet-cel "r.kind != 'Deployment' || r.spec.replicas > 1"`, want: []string{"vet-cel", "r.kind != 'Deployment' || r.spec.replicas > 1"}},
		{line: `set-annotation note 'needs "review" today'`, want: []string{"set-annotation", "note", `needs "review" today`}},
		// So is the value of a named argument.
		{line: `get-cel --expression="r.kind == 'Deployment'" --flag=x`, want: []string{"get-cel", "--expression=r.kind == 'Deployment'", "--flag=x"}},
		{line: `set-yq --param='template:replicas={{ .Params.replicas }}'`, want: []string{"set-yq", "--param=template:replicas={{ .Params.replicas }}"}},
		// A quote inside that whitespace follows would close the argument, so it is escaped.
		{line: `get-cel "r.kind == \"Deployment\" ? r.metadata.name + \"/\" + r.kind : ''"`, want: []string{"get-cel", `r.kind == "Deployment" ? r.metadata.name + "/" + r.kind : ''`}},
		{line: `set-annotation note 'it\'s here'`, want: []string{"set-annotation", "note", "it's here"}},
		// A quote anywhere else is part of the word, as it always was.
		{line: `vet-cel r.kind=="Deployment"`, want: []string{"vet-cel", `r.kind=="Deployment"`}},
		{line: `where-filter --expr=a=="b"`, want: []string{"where-filter", `--expr=a=="b"`}},
		{line: `set-path spec.x {"a":1}`, want: []string{"set-path", "spec.x", `{"a":1}`}},
		{line: `set-annotation note "never closed`, wantErr: true},
	}
	for _, test := range tests {
		got, err := splitFunctionLine(test.line)
		if test.wantErr {
			if err == nil {
				t.Errorf("%q was split into %q", test.line, got)
			}
			continue
		}
		if err != nil || !reflect.DeepEqual(got, test.want) {
			t.Errorf("%q: got %q, %v; want %q", test.line, got, err, test.want)
		}
	}
}

func TestParseFunctionLine(t *testing.T) {
	invocation, err := parseFunctionLine(`get-cel --expression="r.kind == 'Deployment'"`)
	if err != nil {
		t.Fatal(err)
	}
	if invocation.FunctionName != "get-cel" || len(invocation.Arguments) != 1 || *invocation.Arguments[0].ParameterName != "expression" {
		t.Errorf("got %+v", invocation)
	}
	if blank, err := parseFunctionLine("  "); blank != nil || err != nil {
		t.Errorf("a blank line is %+v, %v", blank, err)
	}
}
