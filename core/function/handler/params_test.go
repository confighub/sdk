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
		name     string
		tmpl     string
		want     string
		wantErr  bool
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
