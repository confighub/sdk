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

func registerPatchMutations(fh handler.FunctionRegistry, converter configkit.ConfigConverter, resourceProvider yamlkit.ResourceProvider) {
	fh.RegisterFunction("patch-mutations", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "patch-mutations",
			Parameters: []api.FunctionParameter{
				{
					ParameterName:    "mutation-predicates",
					Required:         true,
					Description:      "Mutations with predicates set to true if they are patchable",
					DataType:         api.DataTypeResourceMutationList,
					ValueConstraints: api.ValueConstraints{Schema: &api.ResourceMutationListSchema},
				},
				{
					ParameterName:    "mutation-patch",
					Required:         true,
					Description:      "Mutations to filter and patch",
					DataType:         api.DataTypeResourceMutationList,
					ValueConstraints: api.ValueConstraints{Schema: &api.ResourceMutationListSchema},
				},
			},
			Mutating:              true,
			Validating:            false,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Selectively patch attributes if their mutations indicate they are patchable. Intended to be used with compute-mutations.",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
			return genericFnPatchMutations(resourceProvider, fArgs.FunctionContext, fArgs.ParsedData, fArgs.Arguments)
		},
	})
}

func genericFnPatchMutations(resourceProvider yamlkit.ResourceProvider, _ *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument) (gaby.Container, any, error) {
	mutationPredicatesString := args[0].Value.(string)
	var mutationsPredicates api.ResourceMutationList
	err := json.Unmarshal([]byte(mutationPredicatesString), &mutationsPredicates)
	if err != nil {
		return parsedData, nil, err
	}
	mutationPatchString := args[1].Value.(string)
	var mutationsPatch api.ResourceMutationList
	err = json.Unmarshal([]byte(mutationPatchString), &mutationsPatch)
	if err != nil {
		return parsedData, nil, err
	}

	parsedData, err = yamlkit.PatchMutations(parsedData, mutationsPredicates, mutationsPatch, resourceProvider)
	return parsedData, nil, err
}
