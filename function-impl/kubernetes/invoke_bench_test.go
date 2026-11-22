// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package kubernetes

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/confighub/sdk/core/function/api"
)

// The handler's cost per invocation is dominated by parsing, converting and serializing the
// configuration data, not by the functions, so these benchmarks invoke cheap functions on a
// large Unit: a Flux install of 38 documents, 13 of them CRDs, 654 kB.
var benchmarkUnitPath = filepath.Join("..", "..", "..", "test-data", "datasets", "confighubops",
	"unit-data", "confighub-prod", "fluxcd.yaml")

func benchmarkUnitData(b *testing.B) string {
	b.Helper()
	data, err := os.ReadFile(benchmarkUnitPath)
	if err != nil {
		b.Skipf("benchmark data not available: %v", err)
	}
	return string(data)
}

func benchmarkInvoke(b *testing.B, data string, invocations ...api.FunctionInvocation) {
	b.Helper()
	benchmarkInvokeRequests(b, data, &api.FunctionInvocationRequest{
		FunctionInvocationOptions: api.FunctionInvocationOptions{StopOnError: true},
		ConfigData:                data,
		FunctionInvocations:       invocations,
	})
}

// benchmarkInvokeRequests makes each request in turn per iteration.
func benchmarkInvokeRequests(b *testing.B, data string, requests ...*api.FunctionInvocationRequest) {
	b.Helper()
	// The handler logs each invocation, which would bury the results.
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	b.Cleanup(func() { slog.SetDefault(previousLogger) })
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	for b.Loop() {
		for _, request := range requests {
			// InvokeCore fills the FunctionContext's previous hash, so each iteration gets a fresh copy.
			req := *request
			resp, err := testFunctionHandler.InvokeCore(context.Background(), &req)
			if err != nil {
				b.Fatal(err)
			}
			if !resp.Success {
				b.Fatalf("invocation failed: %v", resp.ErrorMessages)
			}
		}
	}
}

// A read-only invocation: the data is parsed and nothing is written back.
func BenchmarkInvokeReadOnly(b *testing.B) {
	benchmarkInvoke(b, benchmarkUnitData(b),
		api.FunctionInvocation{FunctionName: "get-resources", Arguments: []api.FunctionArgument{{Value: "json"}}})
}

// get-resources with native bodies, as resource sync invokes it: each document is also
// converted back to the toolchain's form.
func BenchmarkInvokeReadOnlyNativeBodies(b *testing.B) {
	benchmarkInvoke(b, benchmarkUnitData(b),
		api.FunctionInvocation{FunctionName: "get-resources", Arguments: []api.FunctionArgument{{Value: "native"}}})
}

// Several read-only functions in one invocation, as validating Triggers run in one batch: the
// documents are parsed once and each function but the last gets a copy.
func BenchmarkInvokeReadOnlySequence(b *testing.B) {
	getOfType := api.FunctionInvocation{FunctionName: "get-resources-of-type", Arguments: []api.FunctionArgument{
		{Value: "apiextensions.k8s.io/v1/CustomResourceDefinition"}}}
	benchmarkInvoke(b, benchmarkUnitData(b), getOfType, getOfType, getOfType, getOfType)
}

// A mutating invocation that changes every namespaced resource: mutations are computed and
// the data is converted back.
func BenchmarkInvokeMutating(b *testing.B) {
	benchmarkInvoke(b, benchmarkUnitData(b),
		api.FunctionInvocation{FunctionName: "set-namespace", Arguments: []api.FunctionArgument{{Value: "benchmark-ns"}}})
}

// A mutating invocation that finds nothing to change, as most Units do in a fleet-wide edit.
func BenchmarkInvokeMutatingNoChange(b *testing.B) {
	benchmarkInvoke(b, benchmarkUnitData(b),
		api.FunctionInvocation{FunctionName: "set-namespace", Arguments: []api.FunctionArgument{{Value: "flux-system"}}})
}

// The functions a write runs on a Unit's new data, from which its Resources and its needed and
// provided paths are derived.
var derivedReads = []api.FunctionInvocation{
	{FunctionName: "get-resources", Arguments: []api.FunctionArgument{{Value: "json"}}},
	{FunctionName: "get-resources", Arguments: []api.FunctionArgument{{Value: "native"}}},
	{FunctionName: "get-provided"},
	{FunctionName: "get-needed"},
	{FunctionName: "get-references"},
}

// The derived reads invoked one per call: the data is converted and parsed for each.
func BenchmarkDerivedReadsSeparate(b *testing.B) {
	data := benchmarkUnitData(b)
	requests := make([]*api.FunctionInvocationRequest, 0, len(derivedReads))
	for _, invocation := range derivedReads {
		requests = append(requests, &api.FunctionInvocationRequest{
			FunctionInvocationOptions: api.FunctionInvocationOptions{StopOnError: true},
			ConfigData:                data,
			FunctionInvocations:       []api.FunctionInvocation{invocation},
		})
	}
	benchmarkInvokeRequests(b, data, requests...)
}

// The derived reads in one call, with each function's output returned separately: the data is
// converted and parsed once, and each function but the last gets a copy.
func BenchmarkDerivedReadsCombined(b *testing.B) {
	data := benchmarkUnitData(b)
	benchmarkInvokeRequests(b, data, &api.FunctionInvocationRequest{
		ConfigData:           data,
		FunctionInvocations:  derivedReads,
		OutputsPerInvocation: true,
	})
}
