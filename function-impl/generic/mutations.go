// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package generic

import (
	"encoding/json"
	"log/slog"

	"github.com/swaggest/jsonschema-go"

	"github.com/confighub/sdk/core/configkit"
	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/function/handler"
	"github.com/confighub/sdk/core/third_party/gaby"
)

func registerPatchMutations(fh handler.FunctionRegistry, converter configkit.ConfigConverter, resourceProvider yamlkit.ResourceProvider) {
	reflector := jsonschema.Reflector{}
	conflictListSchema, err := reflector.Reflect(api.MutationConflictList{})
	if err != nil {
		slog.Error("couldn't get schema for api.MutationConflictList", "error", err)
	}
	if err := fh.RegisterFunction("patch-mutations", &handler.FunctionRegistration{
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
				{
					ParameterName:    "mutations-to-subtract",
					Required:         false,
					Description:      "Mutations to subtract from the patch (e.g., target-side changes relative to a merge base) before applying. Pass an empty list or omit to skip subtraction.",
					DataType:         api.DataTypeResourceMutationList,
					ValueConstraints: api.ValueConstraints{Schema: &api.ResourceMutationListSchema},
				},
			},
			OutputInfo: &api.FunctionOutput{
				ResultName:  "conflicts",
				Description: "Mutations from the patch that were dropped (subtracted by the target, filtered by a predicate, or unresolvable against the target's data) and any target-side mutations the patch erased. Empty when the patch applied cleanly.",
				OutputType:  api.OutputTypeMutationConflictList,
				Schema:      &conflictListSchema,
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
			return genericFnPatchMutations(resourceProvider, fArgs.FunctionContext, fArgs.ParsedData, fArgs.Arguments, fArgs.Options)
		},
	}); err != nil {
		slog.Error("failed to register function", "error", err)
	}
}

func genericFnPatchMutations(resourceProvider yamlkit.ResourceProvider, _ *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, options *api.FunctionOptions) (gaby.Container, any, error) {
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

	var mutationsToSubtract api.ResourceMutationList
	if len(args) > 2 {
		if subtractString, ok := args[2].Value.(string); ok && subtractString != "" {
			if err := json.Unmarshal([]byte(subtractString), &mutationsToSubtract); err != nil {
				return parsedData, nil, err
			}
		}
	}

	parsedData, conflicts, err := yamlkit.PatchMutations(parsedData, mutationsPredicates, mutationsPatch, mutationsToSubtract, resourceProvider, options)
	if err != nil {
		return parsedData, nil, err
	}
	// Return nil rather than an empty slice when there's nothing to report so
	// the executor doesn't surface an empty Output entry on a clean patch.
	if len(conflicts) == 0 {
		return parsedData, nil, nil
	}
	return parsedData, conflicts, nil
}
