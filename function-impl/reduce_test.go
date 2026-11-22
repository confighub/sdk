// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package impl

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/workerapi"
)

func reduceRows() api.RowList {
	unitA, unitB := uuid.New(), uuid.New()
	return api.RowList{
		{AttributeValue: api.AttributeValue{Value: 2, AttributeInfo: api.AttributeInfo{AttributeIdentifier: api.AttributeIdentifier{Path: "spec.replicas"}}},
			Origin: api.RowOrigin{UnitID: unitA, UnitSlug: "a", SpaceSlug: "dev", SpaceLabels: map[string]string{"Environment": "dev"}}},
		{AttributeValue: api.AttributeValue{Value: 3, AttributeInfo: api.AttributeInfo{AttributeIdentifier: api.AttributeIdentifier{Path: "spec.replicas"}}},
			Origin: api.RowOrigin{UnitID: unitB, UnitSlug: "b", SpaceSlug: "dev", SpaceLabels: map[string]string{"Environment": "dev"}}},
	}
}

const sumReducer = `def reduce(rows):
  total = 0
  units = []
  for r in rows:
    total += r["Value"]
    units.append(r["Origin"]["UnitSlug"])
  return [
    {"Path": "replicas", "Value": total},
    {"Path": "units", "Value": units},
    {"Path": "group", "Value": group},
    {"Path": "label", "Value": rows[0]["Origin"]["SpaceLabels"]["Environment"]},
  ]
`

// A reducer is passed the rows of a group and the group's key, in place of configuration data, and
// whole numbers in the rows stay integers, so a total adds up as integers.
func TestReduceStarlarkSummarizesRows(t *testing.T) {
	executor := NewStandardExecutor(nil, true)
	resp, err := executor.Invoke(context.Background(), &api.FunctionInvocationRequest{
		FunctionContext: api.FunctionContext{ToolchainType: workerapi.ToolchainKubernetesYAML},
		FunctionInvocations: []api.FunctionInvocation{{
			FunctionName: "reduce-starlark",
			Arguments:    []api.FunctionArgument{{Value: sumReducer}},
		}},
		Rows:     reduceRows(),
		GroupKey: "dev",
	})
	require.NoError(t, err)
	require.True(t, resp.Success, "errors: %v", resp.ErrorMessages)

	var values api.AttributeValueList
	require.NoError(t, json.Unmarshal(resp.Outputs[api.OutputTypeAttributeValueList], &values))
	got := map[string]any{}
	for _, value := range values {
		got[string(value.Path)] = value.Value
	}
	assert.Equal(t, map[string]any{
		"replicas": float64(5), // decoded from JSON; the program added integers
		"units":    []any{"a", "b"},
		"group":    "dev",
		"label":    "dev",
	}, got)
}

// A reducer reads only rows, and a function that reads configuration reads no rows, so each is
// refused where it would be passed the other.
func TestReducerAndMapFunctionsAreNotInterchangeable(t *testing.T) {
	executor := NewStandardExecutor(nil, true)

	resp, err := executor.Invoke(context.Background(), &api.FunctionInvocationRequest{
		FunctionContext: api.FunctionContext{ToolchainType: workerapi.ToolchainKubernetesYAML},
		ConfigData:      nginxDeployment,
		FunctionInvocations: []api.FunctionInvocation{{
			FunctionName: "reduce-starlark",
			Arguments:    []api.FunctionArgument{{Value: sumReducer}},
		}},
	})
	require.NoError(t, err)
	assert.False(t, resp.Success)
	assert.Contains(t, resp.ErrorMessages, "invoke reduce-starlark: the function reduces the rows of a group, so it can only be invoked as a reducer")

	resp, err = executor.Invoke(context.Background(), &api.FunctionInvocationRequest{
		FunctionContext:     api.FunctionContext{ToolchainType: workerapi.ToolchainKubernetesYAML},
		FunctionInvocations: []api.FunctionInvocation{{FunctionName: "get-replicas"}},
		Rows:                reduceRows(),
	})
	require.NoError(t, err)
	assert.False(t, resp.Success)
	assert.Contains(t, resp.ErrorMessages, "invoke get-replicas: the function reads configuration data, so it cannot be invoked as a reducer")
}

func TestReduceStarlarkProgramErrors(t *testing.T) {
	executor := NewStandardExecutor(nil, true)
	for name, tc := range map[string]struct {
		program string
		want    string
	}{
		"no reduce":       {program: "def extract(r): return []", want: "starlark program must define a 'reduce' function"},
		"not a list":      {program: "def reduce(rows): return len(rows)", want: "what reduce returns must be a list, got int"},
		"runtime failure": {program: "def reduce(rows): return [rows[0]['Missing']]", want: "starlark error"},
	} {
		t.Run(name, func(t *testing.T) {
			resp, err := executor.Invoke(context.Background(), &api.FunctionInvocationRequest{
				FunctionContext: api.FunctionContext{ToolchainType: workerapi.ToolchainKubernetesYAML},
				FunctionInvocations: []api.FunctionInvocation{{
					FunctionName: "reduce-starlark",
					Arguments:    []api.FunctionArgument{{Value: tc.program}},
				}},
				Rows: reduceRows(),
			})
			require.NoError(t, err)
			assert.False(t, resp.Success)
			require.Len(t, resp.ErrorMessages, 1)
			assert.Contains(t, resp.ErrorMessages[0], tc.want)
		})
	}
}
