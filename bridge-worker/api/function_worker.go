// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package api

import (
	"context"

	funcApi "github.com/confighub/sdk/function/api"
	"github.com/confighub/sdk/workerapi"
)

type FunctionWorker interface {
	Info() FunctionWorkerInfo
	Invoke(FunctionWorkerContext, funcApi.FunctionInvocationRequest) (funcApi.FunctionInvocationResponse, error)
}

type FunctionWorkerInfo struct {
	SupportedFunctions map[workerapi.ToolchainType]map[string]funcApi.FunctionSignature `desccription:"Signatures of supported functions by ToolchainType"`
}

type FunctionWorkerContext interface {
	Context() context.Context
}
