// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package generic

import (
	"encoding/json"

	"github.com/confighub/sdk/configkit"
	"github.com/confighub/sdk/configkit/yamlkit"
	"github.com/confighub/sdk/function/api"
	"github.com/confighub/sdk/function/handler"
	"github.com/confighub/sdk/third_party/gaby"
)

func registerReset(fh handler.FunctionRegistry, converter configkit.ConfigConverter, resourceProvider yamlkit.ResourceProvider) {
	fh.RegisterFunction("reset", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "reset",
			Parameters: []api.FunctionParameter{
				{
					ParameterName:    "mutation-predicates",
					Required:         true,
					Description:      "Mutations with predicates set to true if they should be reset",
					DataType:         api.DataTypeResourceMutationList,
					ValueConstraints: api.ValueConstraints{Schema: &api.ResourceMutationListSchema},
				},
			},
			Mutating:              true,
			Validating:            false,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Sets attributes back to placeholder values if last set by mutations that match the predicates",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
			return genericFnReset(resourceProvider, fArgs.FunctionContext, fArgs.ParsedData, fArgs.Arguments)
		},
	})
}

func genericFnReset(resourceProvider yamlkit.ResourceProvider, _ *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument) (gaby.Container, any, error) {
	mutationPredicatesString := args[0].Value.(string)
	var mutationsPredicates api.ResourceMutationList
	err := json.Unmarshal([]byte(mutationPredicatesString), &mutationsPredicates)
	if err != nil {
		return parsedData, nil, err
	}

	err = yamlkit.Reset(parsedData, mutationsPredicates, resourceProvider)
	return parsedData, nil, err
}
