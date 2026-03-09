// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

// Package executor defines the FunctionExecutor interface used by the worker
// connector to invoke functions without depending on the concrete implementation
// and its heavy dependencies.
package executor

import (
	"context"

	"github.com/confighub/sdk/function/api"
	"github.com/confighub/sdk/workerapi"
)

// FunctionExecutor is the interface that function executors must implement to be
// used with the worker connector. This avoids importing the concrete function
// package (which pulls in all built-in handler implementations and their dependencies).
type FunctionExecutor interface {
	RegisteredFunctions() map[workerapi.ToolchainType]map[string]api.FunctionSignature
	Invoke(ctx context.Context, functionInvocation *api.FunctionInvocationRequest) (*api.FunctionInvocationResponse, error)
}
