// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package handler

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	api "github.com/confighub/sdk/core/function/api"
)

// TestEvaluateTemplate_Params verifies the dual-injection scope: caller-supplied
// params (.Params.x) and FunctionContext fields (promoted, e.g. .UnitSlug) are
// both in scope, and a referenced-but-unsupplied param is a hard error.
func TestEvaluateTemplate_Params(t *testing.T) {
	fc := &api.FunctionContext{UnitSlug: "my-unit", SpaceSlug: "my-space"}
	params := map[string]any{"verb": "create", "role": "app-reader"}

	cases := []struct {
		name    string
		tmpl    string
		want    string
		wantErr bool
	}{
		{name: "param", tmpl: "{{ .Params.verb }}", want: "create"},
		{name: "param in kv", tmpl: "verb={{ .Params.verb }}", want: "verb=create"},
		{name: "function context promoted field", tmpl: "{{ .UnitSlug }}", want: "my-unit"},
		{name: "mixed", tmpl: "{{ .SpaceSlug }}:{{ .Params.role }}", want: "my-space:app-reader"},
		{name: "missing param errors", tmpl: "{{ .Params.namespace }}", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := evaluateTemplate(nil, fc, params, tc.tmpl)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestEvaluateTemplate_NilParams confirms existing FunctionContext-only templates
// keep working when no params are supplied (params == nil).
func TestEvaluateTemplate_NilParams(t *testing.T) {
	fc := &api.FunctionContext{UnitSlug: "u"}
	got, err := evaluateTemplate(nil, fc, nil, "{{ .UnitSlug }}")
	require.NoError(t, err)
	assert.Equal(t, "u", got)
}

// TestEvaluateCEL_Params verifies the params and functionContext CEL variables.
func TestEvaluateCEL_Params(t *testing.T) {
	fc := &api.FunctionContext{UnitSlug: "my-unit"}
	params := map[string]any{"verb": "create"}

	got, err := evaluateCEL(nil, fc, params, `"verb=" + params.verb`)
	require.NoError(t, err)
	assert.Equal(t, "verb=create", got)

	// functionContext is a dyn variable; coerce to string to satisfy the
	// string-output requirement of evaluateCEL.
	got, err = evaluateCEL(nil, fc, params, `string(functionContext.UnitSlug)`)
	require.NoError(t, err)
	assert.Equal(t, "my-unit", got)
}

// TestArgumentEvaluatorsSeeConflicts covers the other half of putting merge conflicts in
// the FunctionContext. A function that reasons about them can read them directly, but a
// function that does not know about them can still be pointed at them through an
// argument, so getting them into the context is the only thing that has to happen.
func TestArgumentEvaluatorsSeeConflicts(t *testing.T) {
	fc := &api.FunctionContext{
		UnitSlug: "my-unit",
		Conflicts: api.MutationConflictList{
			{Reason: api.ConflictReasonUnresolvedPath, Path: "spec.replicas"},
		},
	}

	got, err := evaluateTemplate(nil, fc, nil, "{{ (index .Conflicts 0).Reason }}")
	require.NoError(t, err)
	assert.Equal(t, "UnresolvedPath", got)

	got, err = evaluateCEL(nil, fc, nil, `string(functionContext.Conflicts[0].Path)`)
	require.NoError(t, err)
	assert.Equal(t, "spec.replicas", got)

	// A Unit with no conflicts is the ordinary case, and the field is omitempty, so
	// nothing about the context changes for it.
	clean := &api.FunctionContext{UnitSlug: "my-unit"}
	got, err = evaluateTemplate(nil, clean, nil, "{{ len .Conflicts }}")
	require.NoError(t, err)
	assert.Equal(t, "0", got)
}

// TestEvaluateTemplate_NoHTMLEscaping pins the rendering to text/template. Argument
// values become configuration data, not markup, so the characters HTML would escape have
// to survive verbatim: html/template renders a quote as &#34;, which does not fail — it
// silently writes corrupt configuration that only shows up in a diff. A parameterized
// Invocation supplying a JSON array, a URL with a query string, or a shell redirect all
// depend on this.
func TestEvaluateTemplate_NoHTMLEscaping(t *testing.T) {
	fc := &api.FunctionContext{UnitSlug: "my-unit"}
	params := map[string]any{
		"json":    `["argoproj.io","apps"]`,
		"url":     "https://example.com/a?x=1&y=2",
		"compare": "a < b && c > d",
		"apostro": "it's",
	}

	cases := []struct {
		name string
		tmpl string
		want string
	}{
		{name: "double quotes", tmpl: "{{ .Params.json }}", want: `["argoproj.io","apps"]`},
		{name: "ampersand", tmpl: "{{ .Params.url }}", want: "https://example.com/a?x=1&y=2"},
		{name: "angle brackets", tmpl: "{{ .Params.compare }}", want: "a < b && c > d"},
		{name: "single quote", tmpl: "{{ .Params.apostro }}", want: "it's"},
		{name: "in kv form", tmpl: "verbs={{ .Params.json }}", want: `verbs=["argoproj.io","apps"]`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := evaluateTemplate(nil, fc, params, tc.tmpl)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}
