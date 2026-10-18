// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package channel

import (
	"context"
	"encoding/json"
	"fmt"

	funcapi "github.com/confighub/sdk/core/function/api"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/confighub/sdk/core/workerapi"
)

// Functions on a Channel. The functions protocol rides on one reserved command,
// FunctionInvokeCommand: a Channel that serves functions declares it, with the functions it
// serves by toolchain and name under FunctionAttribute, and every run of it carries one
// FunctionInvocationRequest, the unit's configuration and the functions to run on it in
// order, exactly what a function worker is sent. Users never see the command: cub function do
// --channel runs it. A bot that serves functions implements them as it always has, against
// the function executor's interface, and HandleFunctions does the rest.
//
// The names are the server's; they are repeated here so a client needs nothing from the
// server's packages.
const (
	// FunctionInvokeCommand is the command the functions protocol rides on.
	FunctionInvokeCommand = "confighub.com/function.invoke"
	// FunctionRequestParam is the run's one option: the whole invocation request.
	FunctionRequestParam = "request"
	// FunctionAttribute, on the FunctionInvokeCommand declaration, lists the functions served:
	// {"toolchains": {<toolchain>: {<function>: FunctionSignature}}}.
	FunctionAttribute = "confighub.com/function"
)

// FunctionInvoker executes a function invocation request: what a function executor does. The
// SDK's standard executor and any executor built on it satisfy it.
type FunctionInvoker interface {
	Invoke(ctx context.Context, request *funcapi.FunctionInvocationRequest) (*funcapi.FunctionInvocationResponse, error)
}

// FunctionCommands is the declaration a Channel needs to serve the given functions: the one
// reserved command, with the served signatures by toolchain and name under FunctionAttribute.
// Pipe it into cub channel create or update --commands.
func FunctionCommands(functions map[workerapi.ToolchainType]map[string]funcapi.FunctionSignature) map[string]goclientnew.CommandSignature {
	served := map[string]map[string]funcapi.FunctionSignature{}
	for toolchain, byName := range functions {
		served[string(toolchain)] = byName
	}
	return map[string]goclientnew.CommandSignature{
		FunctionInvokeCommand: {
			Description: "Run functions on this channel's executor.",
			Options: []goclientnew.CommandOption{{
				Name: FunctionRequestParam, DataType: "json", Required: true,
				Description: "the FunctionInvocationRequest: the unit's configuration and context, and the functions to run on it in order",
			}},
			Attributes: map[string]any{FunctionAttribute: map[string]any{"toolchains": served}},
		},
	}
}

// HandleFunctions serves functions on the listener: every run of FunctionInvokeCommand is
// decoded into the invocation request, executed by the invoker, and answered with the
// function response. A request the run does not carry is Rejected; an executor error is
// Failed, with its message.
func (l *Listener) HandleFunctions(invoker FunctionInvoker) *Listener {
	return l.Handle(FunctionInvokeCommand, func(ctx context.Context, run *Run) (json.RawMessage, error) {
		var request funcapi.FunctionInvocationRequest
		found, err := run.Param(FunctionRequestParam, &request)
		if err != nil {
			return nil, Reject("%s is not a function invocation request: %v", FunctionRequestParam, err)
		}
		if !found {
			return nil, Reject("run carries no %s; run functions with cub function do --channel", FunctionRequestParam)
		}
		response, err := invoker.Invoke(ctx, &request)
		if err != nil {
			return nil, fmt.Errorf("executor: %w", err)
		}
		return json.Marshal(response)
	})
}
