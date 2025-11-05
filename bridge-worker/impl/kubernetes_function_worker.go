// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package impl

import (
	"errors"

	"github.com/confighub/sdk/bridge-worker/api"
	"github.com/confighub/sdk/function"
	funcApi "github.com/confighub/sdk/function/api"
	"github.com/confighub/sdk/function/handler"
	"github.com/confighub/sdk/workerapi"
)

type KubernetesFunctionWorker struct {
	fh *handler.FunctionHandler
}

func NewKubernetesFunctionWorker() *KubernetesFunctionWorker {
	fh := handler.NewFunctionHandler()
	function.RegisterKubernetes(fh)
	// Register custom functions
	registerCustomFunctions(fh)
	return &KubernetesFunctionWorker{
		fh: fh,
	}
}

func (fw KubernetesFunctionWorker) Info() api.FunctionWorkerInfo {
	// convert function registration to function signature before sending back
	registeredFunctionsMap := make(map[string]funcApi.FunctionSignature)
	for name, registration := range fw.fh.ListCore() {
		registeredFunctionsMap[name] = registration.FunctionSignature
	}
	return api.FunctionWorkerInfo{
		SupportedFunctions: map[workerapi.ToolchainType]map[string]funcApi.FunctionSignature{
			workerapi.ToolchainKubernetesYAML: registeredFunctionsMap,
		},
	}
}

func (fw KubernetesFunctionWorker) Invoke(workerCtx api.FunctionWorkerContext, request funcApi.FunctionInvocationRequest) (funcApi.FunctionInvocationResponse, error) {
	resp, err := fw.fh.InvokeCore(workerCtx.Context(), &request)
	if err != nil {
		return funcApi.FunctionInvocationResponse{}, err
	}
	if resp == nil {
		return funcApi.FunctionInvocationResponse{}, errors.New("InvokeCore returned nil response")
	}
	return *resp, nil
}

var _ api.FunctionWorker = (*KubernetesFunctionWorker)(nil)
