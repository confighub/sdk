// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"strings"
	"testing"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

// A Unit whose functions ran but whose result was not saved comes back with an Error, and from
// some servers with Success set as well. Either way the invocation failed there: the response
// says so instead of "Config data changed", and the command exits non-zero.
func TestFailedInvocationIsReported(t *testing.T) {
	ok := goclientnew.FunctionInvocationsResponse{UnitSlug: "web", Success: true, Mutators: []int{0}}
	notSaved := goclientnew.FunctionInvocationsResponse{UnitSlug: "api", Success: true, Mutators: []int{0},
		Error: &goclientnew.ResponseError{Message: "Failed to update unit: unit api changed while the functions ran"}}
	failed := goclientnew.FunctionInvocationsResponse{UnitSlug: "db",
		Error: &goclientnew.ResponseError{Message: "function failed"}}

	if err := failedInvocationsError(&[]goclientnew.FunctionInvocationsResponse{ok}); err != nil {
		t.Errorf("no Unit failed, got %v", err)
	}
	err := failedInvocationsError(&[]goclientnew.FunctionInvocationsResponse{ok, notSaved, failed})
	if err == nil || !strings.Contains(err.Error(), "2 of 3") {
		t.Errorf("want the invocation to fail on 2 of 3 units, got %v", err)
	}

	out := captureStdout(t, func() {
		outputFunctionInvocationResponse(&[]goclientnew.FunctionInvocationsResponse{notSaved})
	})
	if !strings.Contains(out, "Function(s) failed on unit") || !strings.Contains(out, "changed while the functions ran") {
		t.Errorf("want the failure and its reason, got:\n%s", out)
	}
	if strings.Contains(out, "Config data changed") {
		t.Errorf("a result that was not saved changed nothing, got:\n%s", out)
	}
}
