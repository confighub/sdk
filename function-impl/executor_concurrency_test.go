// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package impl

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/workerapi"
)

const nginxDeployment = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
spec:
  replicas: 1
  selector:
    matchLabels:
      app: web
  template:
    metadata:
      labels:
        app: web
    spec:
      containers:
      - name: web
        image: nginx:1.25.0
`

// TestExecutorsAreBuiltWhileFunctionsRun covers a process that holds more than one executor, as
// a server does that builds one per Space on first use and again when the Space's attributes
// change, with functions running on other goroutines throughout. Building an executor may write
// only to the executor being built: anything it stores in a package variable is written by every
// build and read by every running function. The assertions here cannot see that; -race, which
// the tests run under, reports it.
func TestExecutorsAreBuiltWhileFunctionsRun(t *testing.T) {
	running := NewStandardExecutor(nil, true)

	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			NewStandardExecutor(nil, true)
		})
		wg.Go(func() {
			resp, err := running.Invoke(context.Background(), &api.FunctionInvocationRequest{
				FunctionContext: api.FunctionContext{ToolchainType: workerapi.ToolchainKubernetesYAML},
				ConfigData:      nginxDeployment,
				FunctionInvocations: []api.FunctionInvocation{{
					FunctionName: "set-image-reference-by-uri",
					Arguments:    []api.FunctionArgument{{Value: "nginx"}, {Value: ":1.27.0"}},
				}},
			})
			if assert.NoError(t, err) {
				assert.True(t, resp.Success, "set-image-reference-by-uri should succeed; errors: %v", resp.ErrorMessages)
				assert.Contains(t, resp.ConfigData, "nginx:1.27.0")
			}
		})
	}
	wg.Wait()
}
