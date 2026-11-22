// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package handler

import (
	"context"
	"strings"
	"testing"

	"github.com/cockroachdb/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/confighub/sdk/core/configkit/cubkit"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/third_party/gaby"
)

// The handler parses the data once per invocation and hands the same documents from one
// function to the next, where it used to parse again before each function. These tests pin the
// guarantees the re-parse used to give for free.

const parseOnceData = `# leading comment
apiVersion: confighub.com/v1
kind: Space
metadata:
  name: staging # inline comment
spec:
  labels:
    Environment: staging
`

func setOnEveryDoc(docs gaby.Container, path, value string) {
	for _, doc := range docs {
		_, _ = doc.SetP(value, path)
	}
}

func parseOnceHandler(t *testing.T) *FunctionHandler {
	t.Helper()
	fh := NewFunctionHandler(cubkit.NewConfigHubResourceProvider())
	register := func(name string, mutating bool, fn FunctionImplementation) {
		require.NoError(t, fh.RegisterFunction(name, &FunctionRegistration{
			FunctionSignature: api.FunctionSignature{
				FunctionName:          name,
				Mutating:              mutating,
				Parameters:            []api.FunctionParameter{{ParameterName: "value", DataType: api.DataTypeString}},
				AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
			},
			Function: fn,
		}))
	}
	// A mutating function that sets a label to its argument.
	register("set-test-label", true, func(args FunctionImplementationArguments) (gaby.Container, any, error) {
		setOnEveryDoc(args.ParsedData, "spec.labels.Test", args.Arguments[0].Value.(string))
		return args.ParsedData, nil, nil
	})
	// A function that does not declare itself mutating but edits its documents anyway.
	register("misbehaving-reader", false, func(args FunctionImplementationArguments) (gaby.Container, any, error) {
		setOnEveryDoc(args.ParsedData, "spec.labels.Leaked", args.Arguments[0].Value.(string))
		return args.ParsedData, nil, nil
	})
	// A mutating function that edits its documents and then fails.
	register("fail-after-edit", true, func(args FunctionImplementationArguments) (gaby.Container, any, error) {
		setOnEveryDoc(args.ParsedData, "spec.labels.Partial", args.Arguments[0].Value.(string))
		return args.ParsedData, nil, errors.New("failed after editing")
	})
	return fh
}

func invokeParseOnce(t *testing.T, fh *FunctionHandler, stopOnError bool, invocations ...api.FunctionInvocation) *api.FunctionInvocationResponse {
	t.Helper()
	resp, err := fh.InvokeCore(context.Background(), &api.FunctionInvocationRequest{
		FunctionInvocationOptions: api.FunctionInvocationOptions{StopOnError: stopOnError},
		ConfigData:                parseOnceData,
		FunctionInvocations:       invocations,
	})
	require.NoError(t, err)
	return resp
}

func call(name, value string) api.FunctionInvocation {
	return api.FunctionInvocation{FunctionName: name, Arguments: []api.FunctionArgument{{Value: value}}}
}

// A function that does not mutate must not change what a later mutating function sees, nor the
// result, whether it runs before or after the mutating one.
func TestNonMutatingFunctionEditsDoNotLeak(t *testing.T) {
	fh := parseOnceHandler(t)
	resp := invokeParseOnce(t, fh, true,
		call("misbehaving-reader", "before"),
		call("set-test-label", "value"),
		call("misbehaving-reader", "after"),
	)
	require.True(t, resp.Success, resp.ErrorMessages)
	assert.Equal(t, []int{1}, resp.Mutators)
	assert.Contains(t, resp.ConfigData, "Test: value")
	assert.NotContains(t, resp.ConfigData, "Leaked")
	// Comments survive, converted back from keys.
	assert.Contains(t, resp.ConfigData, "# leading comment")
	assert.Contains(t, resp.ConfigData, "# inline comment")
	for _, mutation := range resp.Mutations {
		for path := range mutation.PathMutationMap {
			assert.NotContains(t, string(path), "Leaked")
		}
	}
}

// Without a mutating function nothing is returned, so the functions share one parse; a
// misbehaving reader's edits are still not returned.
func TestReadOnlySequenceReturnsNoData(t *testing.T) {
	fh := parseOnceHandler(t)
	resp := invokeParseOnce(t, fh, true, call("misbehaving-reader", "x"), call("misbehaving-reader", "y"))
	require.True(t, resp.Success, resp.ErrorMessages)
	assert.Empty(t, resp.Mutators)
	assert.Empty(t, resp.ConfigData)
	assert.Equal(t, api.HashConfigDataSHA256(parseOnceData), resp.DataHash)
}

// A mutating function that fails with StopOnError has edited the documents in place before
// failing; the result is the data as the last successful mutation left it.
func TestFailedMutatingFunctionEditsAreDropped(t *testing.T) {
	fh := parseOnceHandler(t)
	resp := invokeParseOnce(t, fh, true, call("set-test-label", "value"), call("fail-after-edit", "partial"))
	assert.False(t, resp.Success)
	assert.Equal(t, []int{0}, resp.Mutators)
	assert.Contains(t, resp.ConfigData, "Test: value")
	assert.NotContains(t, resp.ConfigData, "Partial")
	assert.Contains(t, resp.ConfigData, "# leading comment")
}

// Without StopOnError, a failed mutating function's edits are kept, as they always were.
func TestFailedMutatingFunctionEditsAreKeptWithoutStopOnError(t *testing.T) {
	fh := parseOnceHandler(t)
	resp := invokeParseOnce(t, fh, false, call("fail-after-edit", "partial"), call("set-test-label", "value"))
	assert.False(t, resp.Success)
	assert.Equal(t, []int{0, 1}, resp.Mutators)
	assert.Contains(t, resp.ConfigData, "Partial: partial")
	assert.Contains(t, resp.ConfigData, "Test: value")
}

// A mutating function that changes nothing is not a mutator and returns no data.
func TestNoOpMutationReturnsNoData(t *testing.T) {
	fh := parseOnceHandler(t)
	resp := invokeParseOnce(t, fh, true, call("set-test-label", "value"))
	require.True(t, resp.Success, resp.ErrorMessages)
	again, err := fh.InvokeCore(context.Background(), &api.FunctionInvocationRequest{
		FunctionInvocationOptions: api.FunctionInvocationOptions{StopOnError: true},
		ConfigData:                resp.ConfigData,
		FunctionInvocations:       []api.FunctionInvocation{call("set-test-label", "value")},
	})
	require.NoError(t, err)
	assert.Empty(t, again.Mutators)
	assert.Empty(t, again.ConfigData)
	assert.Equal(t, resp.DataHash, again.DataHash)
}

// Data that does not parse is reported as it was before: when the first function runs.
func TestUnparseableDataIsReportedWhenAFunctionRuns(t *testing.T) {
	fh := parseOnceHandler(t)
	_, err := fh.InvokeCore(context.Background(), &api.FunctionInvocationRequest{
		ConfigData:          "a: [unclosed\n",
		FunctionInvocations: []api.FunctionInvocation{call("set-test-label", "value")},
	})
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "configuration data parsing error"), err.Error())
}

// Every function is given exactly the documents that parsing the converted data before it would
// give, node styles included: after a mutating function, after a copy, and when it is the last.
// The flow mapping is where that matters: converting the data writes nginx:2 single-quoted, so
// parsing it gives a quoted scalar where parsing the original would give a plain one, and a value
// rendered on its own -- as mutation records and getters render them -- differs between the two.
func TestFunctionsSeeTheConvertedDataAsParsed(t *testing.T) {
	const flowData = `apiVersion: confighub.com/v1
kind: Space
metadata:
  name: staging
spec:
  labels: {Image: nginx:2}
`
	fh := parseOnceHandler(t)
	var seen []string
	require.NoError(t, fh.RegisterFunction("record-image", &FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName:          "record-image",
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(args FunctionImplementationArguments) (gaby.Container, any, error) {
			seen = append(seen, args.ParsedData[0].Path("spec.labels.Image").String())
			return args.ParsedData, nil, nil
		},
	}))

	// A mutating function writes a plain scalar, as code assigning a node's Value does, where
	// parsing its output would quote it.
	setPlain := func(docs gaby.Container) {
		node := docs[0].Path("spec.labels.Image").YNode()
		node.Value = "nginx:3"
		node.Style = 0
	}
	require.NoError(t, fh.RegisterFunction("set-image", &FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName:          "set-image",
			Mutating:              true,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(args FunctionImplementationArguments) (gaby.Container, any, error) {
			setPlain(args.ParsedData)
			return args.ParsedData, nil, nil
		},
	}))

	converted, err := fh.GetConverter().NativeToYAML([]byte(flowData))
	require.NoError(t, err)
	parsed, err := gaby.ParseAll(converted)
	require.NoError(t, err)
	want := parsed[0].Path("spec.labels.Image").String()
	require.Contains(t, want, "'", "the converted data quotes the scalar, which is what this test is about")
	setPlain(parsed)
	require.NotContains(t, parsed[0].Path("spec.labels.Image").String(), "'", "the new scalar is plain until parsed")
	reparsed, err := gaby.ParseAll([]byte(parsed.String()))
	require.NoError(t, err)
	wantAfter := reparsed[0].Path("spec.labels.Image").String()
	require.Contains(t, wantAfter, "'")

	resp, err := fh.InvokeCore(context.Background(), &api.FunctionInvocationRequest{
		FunctionInvocationOptions: api.FunctionInvocationOptions{StopOnError: true},
		ConfigData:                flowData,
		FunctionInvocations: []api.FunctionInvocation{
			{FunctionName: "record-image"},
			{FunctionName: "record-image"},
			{FunctionName: "set-image"},
			{FunctionName: "record-image"},
			{FunctionName: "record-image"},
		},
	})
	require.NoError(t, err)
	require.True(t, resp.Success, resp.ErrorMessages)
	assert.Equal(t, []string{want, want, wantAfter, wantAfter}, seen)
}
