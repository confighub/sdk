// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package generic

import (
	"encoding/json"
	"log/slog"

	"github.com/confighub/sdk/core/configkit"
	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/function/handler"
	"github.com/confighub/sdk/core/third_party/gaby"
)

func registerReset(fh handler.FunctionRegistry, converter configkit.ConfigConverter, resourceProvider yamlkit.ResourceProvider) {
	if err := fh.RegisterFunction("reset", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "reset",
			Parameters: []api.FunctionParameter{
				{
					ParameterName:    "mutation-protection",
					Required:         true,
					Description:      "The unit's MutationSources; paths that are not Protected are reset",
					DataType:         api.DataTypeResourceMutationList,
					ValueConstraints: api.ValueConstraints{Schema: &api.ResourceMutationListSchema},
				},
			},
			Mutating:              true,
			Validating:            false,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Sets attributes back to placeholder values if last set by mutations that are not protected",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
			return genericFnReset(resourceProvider, fArgs.FunctionContext, fArgs.ParsedData, fArgs.Arguments, fArgs.Options)
		},
	}); err != nil {
		slog.Error("failed to register function", "error", err)
	}
}

func genericFnReset(resourceProvider yamlkit.ResourceProvider, _ *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, options *api.FunctionOptions) (gaby.Container, any, error) {
	mutationProtectionString := args[0].Value.(string)
	var mutationsProtection api.ResourceMutationList
	err := json.Unmarshal([]byte(mutationProtectionString), &mutationsProtection)
	if err != nil {
		return parsedData, nil, err
	}

	err = yamlkit.Reset(parsedData, mutationsProtection, resourceProvider, options)
	return parsedData, nil, err
}
