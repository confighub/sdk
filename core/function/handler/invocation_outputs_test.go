// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package handler

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/confighub/sdk/core/configkit/cubkit"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/third_party/gaby"
)

func invocationOutputsHandler(t *testing.T, includeDetailsSeen *[]bool) *FunctionHandler {
	t.Helper()
	fh := NewFunctionHandler(cubkit.NewConfigHubResourceProvider())
	// Returns one resource whose body is its argument, so two invocations return the same type.
	require.NoError(t, fh.RegisterFunction("emit-resource", &FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName:          "emit-resource",
			Parameters:            []api.FunctionParameter{{ParameterName: "body", DataType: api.DataTypeString}},
			OutputInfo:            &api.FunctionOutput{ResultName: "resources", OutputType: api.OutputTypeResourceList},
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(args FunctionImplementationArguments) (gaby.Container, any, error) {
			return args.ParsedData, api.ResourceList{{ResourceBody: args.Arguments[0].Value.(string)}}, nil
		},
	}))
	// Returns no output.
	require.NoError(t, fh.RegisterFunction("no-output", &FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName:          "no-output",
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(args FunctionImplementationArguments) (gaby.Container, any, error) {
			return args.ParsedData, nil, nil
		},
	}))
	// Sets IncludeDetails on its options, as the needs/provides getters do, and records what it
	// was given.
	require.NoError(t, fh.RegisterFunction("want-details", &FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName:          "want-details",
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(args FunctionImplementationArguments) (gaby.Container, any, error) {
			*includeDetailsSeen = append(*includeDetailsSeen, api.GetIncludeDetails(args.Options))
			args.Options.IncludeDetails = true
			return args.ParsedData, nil, nil
		},
	}))
	return fh
}

func decodeResources(t *testing.T, outputs map[api.OutputType][]byte) api.ResourceList {
	t.Helper()
	var resources api.ResourceList
	require.NoError(t, json.Unmarshal(outputs[api.OutputTypeResourceList], &resources))
	return resources
}

func TestOutputsPerInvocation(t *testing.T) {
	var seen []bool
	fh := invocationOutputsHandler(t, &seen)
	invocations := []api.FunctionInvocation{
		{FunctionName: "emit-resource", Arguments: []api.FunctionArgument{{Value: "first"}}},
		{FunctionName: "no-output"},
		{FunctionName: "emit-resource", Arguments: []api.FunctionArgument{{Value: "second"}}},
	}

	combined, err := fh.InvokeCore(context.Background(), &api.FunctionInvocationRequest{
		FunctionInvocationOptions: api.FunctionInvocationOptions{StopOnError: true},
		ConfigData:                parseOnceData,
		FunctionInvocations:       invocations,
	})
	require.NoError(t, err)
	require.True(t, combined.Success, combined.ErrorMessages)
	assert.Nil(t, combined.InvocationOutputs)
	assert.Len(t, decodeResources(t, combined.Outputs), 2, "combined, the two lists are one")

	separate, err := fh.InvokeCore(context.Background(), &api.FunctionInvocationRequest{
		FunctionInvocationOptions: api.FunctionInvocationOptions{StopOnError: true},
		ConfigData:                parseOnceData,
		FunctionInvocations:       invocations,
		OutputsPerInvocation:      true,
	})
	require.NoError(t, err)
	require.True(t, separate.Success, separate.ErrorMessages)
	assert.Empty(t, separate.Outputs)
	require.Len(t, separate.InvocationOutputs, 3)
	assert.Equal(t, api.ResourceList{{ResourceBody: "first"}}, decodeResources(t, separate.InvocationOutputs[0]))
	assert.Nil(t, separate.InvocationOutputs[1])
	assert.Equal(t, api.ResourceList{{ResourceBody: "second"}}, decodeResources(t, separate.InvocationOutputs[2]))
}

// A function that does not run, after StopOnError stopped the sequence, has no output.
func TestOutputsPerInvocationAfterStop(t *testing.T) {
	var seen []bool
	fh := invocationOutputsHandler(t, &seen)
	resp, err := fh.InvokeCore(context.Background(), &api.FunctionInvocationRequest{
		FunctionInvocationOptions: api.FunctionInvocationOptions{StopOnError: true},
		ConfigData:                parseOnceData,
		FunctionInvocations: []api.FunctionInvocation{
			{FunctionName: "emit-resource", Arguments: []api.FunctionArgument{{Value: "first"}}},
			{FunctionName: "no-such-function"},
			{FunctionName: "emit-resource", Arguments: []api.FunctionArgument{{Value: "second"}}},
		},
		OutputsPerInvocation: true,
	})
	require.NoError(t, err)
	assert.False(t, resp.Success)
	require.Len(t, resp.InvocationOutputs, 3)
	assert.NotNil(t, resp.InvocationOutputs[0])
	assert.Nil(t, resp.InvocationOutputs[1])
	assert.Nil(t, resp.InvocationOutputs[2])
}

// An option one function sets is not seen by the functions after it.
func TestFunctionOptionsAreNotShared(t *testing.T) {
	var seen []bool
	fh := invocationOutputsHandler(t, &seen)
	resp, err := fh.InvokeCore(context.Background(), &api.FunctionInvocationRequest{
		FunctionInvocationOptions: api.FunctionInvocationOptions{StopOnError: true},
		ConfigData:                parseOnceData,
		FunctionInvocations: []api.FunctionInvocation{
			{FunctionName: "want-details"},
			{FunctionName: "want-details"},
		},
	})
	require.NoError(t, err)
	require.True(t, resp.Success, resp.ErrorMessages)
	assert.Equal(t, []bool{false, false}, seen)
}
