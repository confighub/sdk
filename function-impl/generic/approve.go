// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package generic

import (
	"log/slog"

	"github.com/confighub/sdk/core/configkit"
	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/function/handler"
	"github.com/confighub/sdk/core/third_party/gaby"
)

// TODO: Deprecated in favor of vet-approvedby. Remove this.
func registerIsApproved(fh handler.FunctionRegistry, converter configkit.ConfigConverter, resourceProvider yamlkit.ResourceProvider) {
	if err := fh.RegisterFunction("is-approved", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "is-approved",
			Parameters: []api.FunctionParameter{
				{
					ParameterName: "num-approvers",
					Required:      true,
					Description:   "Number of approvers",
					DataType:      api.DataTypeInt,
					Example:       "2",
				},
			},
			OutputInfo: &api.FunctionOutput{
				ResultName:  "passed",
				Description: "True if approvers are present, false otherwise",
				OutputType:  api.OutputTypeValidationResult,
				Schema:      &api.ValidationResultListSchema,
			},
			Mutating:              false,
			Validating:            true,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "[Deprecated; use vet-approvedby instead] Returns true if sufficient approvers are present",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
			return genericFnVetApprovedBy(resourceProvider, fArgs.FunctionContext, fArgs.ParsedData, fArgs.Arguments)
		},
	}); err != nil {
		slog.Error("failed to register function", "error", err)
	}
}

func registerVetApprovedBy(fh handler.FunctionRegistry, converter configkit.ConfigConverter, resourceProvider yamlkit.ResourceProvider) {
	if err := fh.RegisterFunction("vet-approvedby", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "vet-approvedby",
			Parameters: []api.FunctionParameter{
				{
					ParameterName: "num-approvers",
					Required:      true,
					Description:   "Number of approvers",
					DataType:      api.DataTypeInt,
				},
			},
			OutputInfo: &api.FunctionOutput{
				ResultName:  "passed",
				Description: "True if approvers are present, false otherwise",
				OutputType:  api.OutputTypeValidationResult,
				Schema:      &api.ValidationResultListSchema,
			},
			Mutating:              false,
			Validating:            true,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Returns true if sufficient approvers are present",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
			return genericFnVetApprovedBy(resourceProvider, fArgs.FunctionContext, fArgs.ParsedData, fArgs.Arguments)
		},
	}); err != nil {
		slog.Error("failed to register function", "error", err)
	}
}

func genericFnVetApprovedBy(resourceProvider yamlkit.ResourceProvider, functionContext *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument) (gaby.Container, any, error) {
	numApprovers := args[0].Value.(int)

	// If the data has changed, previous approvers should be cleared.
	// TODO: This doesn't work due to conversion. Also, there are cases where
	// changes by needs/provides should be allowed.
	// if api.ConfigDataHasChanged(functionContext, parsedData.Bytes()) {
	// 	return parsedData, api.ValidationResultFalse, nil
	// }

	if len(functionContext.ApprovedBy) >= numApprovers {
		return parsedData, api.ValidationResultTrue, nil
	}
	return parsedData, api.ValidationResultFalse, nil
}
