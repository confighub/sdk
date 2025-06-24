// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package impl

import (
	"github.com/confighub/sdk/function/api"
	"github.com/confighub/sdk/function/handler"
	"github.com/confighub/sdk/third_party/gaby"
)

func registerCustomFunctions(fh *handler.FunctionHandler) {
	fh.RegisterFunction("echo", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "echo",
			OutputInfo: &api.FunctionOutput{
				ResultName:  "resource",
				Description: "Return the same data as input",
				OutputType:  api.OutputTypeYAML,
			},
			Mutating:    true,
			Validating:  false,
			Hermetic:    true,
			Idempotent:  true,
			Description: "Echo is to demonstrate that a custom function can be registered via the worker model.",
		},
		Function: k8sFnEcho,
	})
}

// k8sFnEcho is a custom function that returns the same data as input.
// It is used to test the function worker to demonstrate that a custom function can be registered and used.
func k8sFnEcho(_ *api.FunctionContext, container gaby.Container, _ []api.FunctionArgument, _ []byte) (gaby.Container, any, error) {
	return container, nil, nil
}
