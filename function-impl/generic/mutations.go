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
	"github.com/labstack/gommon/log"
	"github.com/swaggest/jsonschema-go"
)

func RegisterComputeMutations(fh handler.FunctionRegistry, converter configkit.ConfigConverter, resourceProvider yamlkit.ResourceProvider) {
	reflector := jsonschema.Reflector{}
	resourceMutationListSchema, err := reflector.Reflect(api.ResourceMutationList{})
	if err != nil {
		log.Errorf("couldn't get schema for api.ResourceMutationList")
	}
	fh.RegisterFunction("compute-mutations", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "compute-mutations",
			Parameters: []api.FunctionParameter{
				{
					ParameterName: "config-doc-list",
					Required:      true,
					Description:   "Document list with the previous config data",
					DataType:      converter.DataType(),
				},
				{
					ParameterName: "function-index",
					Required:      true,
					Description:   "Index of the function from the invocation list that mutated the config data",
					DataType:      api.DataTypeInt,
					Example:       "0",
				},
				{
					ParameterName: "already-converted",
					Required:      false,
					Description:   "If true, the config-doc-list is already converted to YAML",
					DataType:      api.DataTypeBool,
				},
			},
			OutputInfo: &api.FunctionOutput{
				ResultName:  "mutations",
				Description: "List of resource mutations in the same order as the resources in the config data",
				OutputType:  api.OutputTypeResourceMutationList,
				Schema:      &resourceMutationListSchema,
			},
			Mutating:              false,
			Validating:            false,
			Hermetic:              true,
			Idempotent:            true,
			Description:           `Diffs the previous config data from the parameter with the current config data from the unit and returns a list of resource mutations made to the config data. The output can be used with patch-mutations.`,
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
			return genericFnComputeMutations(converter, resourceProvider, fArgs.FunctionContext, fArgs.ParsedData, fArgs.Arguments)
		},
	})
}

func genericFnComputeMutations(converter configkit.ConfigConverter, resourceProvider yamlkit.ResourceProvider, _ *api.FunctionContext, modifiedParsedData gaby.Container, args []api.FunctionArgument) (gaby.Container, any, error) {
	configStringData := args[0].Value.(string)
	functionIndex := int64(args[1].Value.(int))
	alreadyConverted := false
	if len(args) > 2 {
		alreadyConverted = args[2].Value.(bool)
	}

	var err error
	yamlData := []byte(configStringData)
	if !alreadyConverted {
		yamlData, err = converter.NativeToYAML(yamlData)
		if err != nil {
			return modifiedParsedData, nil, err
		}
	}
	previousParsedData, err := gaby.ParseAll(yamlData)
	if err != nil {
		return modifiedParsedData, nil, err
	}

	mutations, err := yamlkit.ComputeMutations(previousParsedData, modifiedParsedData, functionIndex, resourceProvider)
	return modifiedParsedData, mutations, err
}

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
					ValueConstraints: api.ValueConstraints{Schema: &resourceMutationListSchema},
				},
				{
					ParameterName:    "mutation-patch",
					Required:         true,
					Description:      "Mutations to filter and patch",
					DataType:         api.DataTypeResourceMutationList,
					ValueConstraints: api.ValueConstraints{Schema: &resourceMutationListSchema},
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
