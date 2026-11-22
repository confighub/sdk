// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"reflect"
	"testing"

	"github.com/confighub/sdk/core/function/api"
)

func TestCanonicalFunctionInvocation(t *testing.T) {
	got := canonicalFunctionInvocation("reduce-starlark", []string{"def reduce(rows): return []", "cel:params.x", "k=v"})
	want := api.FunctionInvocation{FunctionName: "reduce-starlark", Arguments: []api.FunctionArgument{
		{Value: "def reduce(rows): return []"},
		{Value: "params.x", Evaluator: "cel"},
		{Value: "k=v"},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("positional: got %+v, want %+v", got, want)
	}

	got = canonicalFunctionInvocation("get-starlark", []string{"--program=def extract(r): return []", "--param=a=b"})
	want = api.FunctionInvocation{FunctionName: "get-starlark", Arguments: []api.FunctionArgument{
		{ParameterName: "program", Value: "def extract(r): return []"},
		{ParameterName: "param", Value: "a=b"},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("named: got %+v, want %+v", got, want)
	}
}
