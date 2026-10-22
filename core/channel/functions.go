// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package channel

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

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

// FunctionExecutor is a FunctionInvoker that says which functions it serves, as the SDK's
// function executors do. HandleFunctions declares those functions on the Channel.
type FunctionExecutor interface {
	FunctionInvoker
	RegisteredFunctions() map[workerapi.ToolchainType]map[string]funcapi.FunctionSignature
}

// FunctionCommands is the declaration a Channel needs to serve the given functions: the one
// reserved command, with the served signatures by toolchain and name under FunctionAttribute.
// HandleFunctions declares it for an executor; it is exported for declaring out of band, with
// cub channel create or update --commands.
func FunctionCommands(functions map[workerapi.ToolchainType]map[string]funcapi.FunctionSignature) map[string]goclientnew.CommandSignature {
	served := map[string]map[string]funcapi.FunctionSignature{}
	for toolchain, byName := range functions {
		ordered := make(map[string]funcapi.FunctionSignature, len(byName))
		for name, signature := range byName {
			signature.AffectedResourceTypes = canonicalResourceTypes(signature.AffectedResourceTypes)
			ordered[name] = signature
		}
		served[string(toolchain)] = ordered
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
// Failed, with its message. An invoker that is a FunctionExecutor also has its functions
// declared on the Channel when the listener starts (Declare), as a function worker's were
// registered when it connected.
func (l *Listener) HandleFunctions(invoker FunctionInvoker) *Listener {
	if executor, ok := invoker.(FunctionExecutor); ok {
		l.Declare(FunctionCommands(executor.RegisteredFunctions()))
	}
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

// canonicalResourceTypes puts a function's resource types in one form, so the declaration is the
// same in every process and a restarted bot finds it current. The executors build the list from
// map iteration: its order varies, and when the wildcard is among the types the list stops at
// it, so which types precede it varies too. The wildcard means every type, so it stands alone.
func canonicalResourceTypes(types []funcapi.ResourceType) []funcapi.ResourceType {
	if slices.Contains(types, funcapi.ResourceTypeAny) {
		return []funcapi.ResourceType{funcapi.ResourceTypeAny}
	}
	return slices.Sorted(slices.Values(types))
}
