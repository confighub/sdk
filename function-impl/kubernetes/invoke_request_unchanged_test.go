// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package kubernetes

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/confighub/sdk/core/function/api"
)

// TestInvokeCoreLeavesTheRequestUnchanged covers a caller that runs one invocation list against
// many Units at once, as the server's where_data search does: it hands the same list to a
// goroutine per Unit. Validation names positional arguments and converts values, and if it did
// that in the caller's list, those goroutines would be writing the same memory -- a torn
// ParameterName is a nil string pointer with a nonzero length, which crashes the lookup of it.
// Under -race the concurrent calls report such a write directly; without -race, the comparison
// afterwards still catches it.
func TestInvokeCoreLeavesTheRequestUnchanged(t *testing.T) {
	invocations := []api.FunctionInvocation{{
		FunctionName: "where-filter",
		Arguments: []api.FunctionArgument{
			{Value: "apps/v1/Deployment"},
			{Value: "spec.replicas > 0"},
		},
	}}

	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			resp, err := testFunctionHandler.InvokeCore(context.Background(), &api.FunctionInvocationRequest{
				ConfigData:          twoDeploymentsAndService,
				FunctionInvocations: invocations,
			})
			if assert.NoError(t, err) {
				assert.True(t, resp.Success, "where-filter should succeed; errors: %v", resp.ErrorMessages)
			}
		})
	}
	wg.Wait()

	assert.Equal(t, []api.FunctionArgument{
		{Value: "apps/v1/Deployment"},
		{Value: "spec.replicas > 0"},
	}, invocations[0].Arguments, "InvokeCore modified the caller's arguments")
}
