// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package handler

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	api "github.com/confighub/sdk/core/function/api"
)

// A stored invocation names a variadic parameter's arguments apart with suffixes; the function is
// given the parameter's own name for each.
func TestValidateAndBuildArgumentsAcceptsSuffixes(t *testing.T) {
	signature := &api.FunctionSignature{
		FunctionName:       "set-values",
		RequiredParameters: 1,
		VarArgs:            true,
		Parameters: []api.FunctionParameter{
			{ParameterName: "key", Required: true, DataType: api.DataTypeString},
			{ParameterName: "values", DataType: api.DataTypeString},
		},
	}
	invocation := &api.FunctionInvocation{FunctionName: "set-values", Arguments: []api.FunctionArgument{
		{ParameterName: "key", Value: "k"},
		{ParameterName: "values", Value: "a"},
		{ParameterName: "values#2", Value: "b"},
	}}
	arguments, err := ValidateAndBuildArguments(nil, &api.FunctionContext{}, invocation, signature)
	require.NoError(t, err)
	require.Len(t, arguments, 3)
	assert.Equal(t, "values", arguments[2].ParameterName)
	assert.Equal(t, "b", arguments[2].Value)
}
