// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package handler

import (
	"github.com/cockroachdb/errors"

	"github.com/confighub/sdk/core/configkit"
	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/third_party/gaby"
)

func (fh *FunctionHandler) registerComputeMutations() {
	converter := fh.converter
	resourceProvider := fh.resourceProvider

	api.InitTypeSchemas()
	fh.RegisterFunction("compute-mutations", &FunctionRegistration{
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
				{
					ParameterName: "reverse",
					Required:      false,
					Description:   "If true, treat the previous config data as the modified config data instead",
					DataType:      api.DataTypeBool,
				},
			},
			OutputInfo: &api.FunctionOutput{
				ResultName:  "mutations",
				Description: "List of resource mutations in the same order as the resources in the config data",
				OutputType:  api.OutputTypeResourceMutationList,
				Schema:      &api.ResourceMutationListSchema,
			},
			Mutating:              false,
			Validating:            false,
			Hermetic:              true,
			Idempotent:            true,
			Description:           `Diffs the previous config data from the parameter with the current config data from the unit and returns a list of resource mutations made to the config data. The output can be used with patch-mutations.`,
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(fArgs FunctionImplementationArguments) (gaby.Container, any, error) {
			return computeMutations(converter, resourceProvider, fArgs.ParsedData, fArgs.Arguments)
		},
	})
}

func computeMutations(converter configkit.ConfigConverter, resourceProvider yamlkit.ResourceProvider, modifiedParsedData gaby.Container, args []api.FunctionArgument) (gaby.Container, any, error) {
	configStringData := args[0].Value.(string)
	functionIndex := int64(args[1].Value.(int))
	alreadyConverted := false
	reverse := false
	if len(args) > 2 {
		if args[2].ParameterName == "already-converted" || args[2].ParameterName == "" {
			alreadyConverted = args[2].Value.(bool)
		} else if args[2].ParameterName == "reverse" {
			reverse = args[2].Value.(bool)
		} else {
			return modifiedParsedData, nil, errors.Errorf("invalid parameter " + args[2].ParameterName)
		}
		if len(args) > 3 {
			if args[3].ParameterName == "reverse" || args[3].ParameterName == "" {
				reverse = args[3].Value.(bool)
			} else if args[3].ParameterName == "already-converted" {
				alreadyConverted = args[3].Value.(bool)
			} else {
				return modifiedParsedData, nil, errors.Errorf("invalid parameter " + args[2].ParameterName)
			}
		}
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

	var mutations api.ResourceMutationList
	if reverse {
		mutations, err = yamlkit.ComputeMutations(modifiedParsedData, previousParsedData, functionIndex, resourceProvider)
	} else {
		mutations, err = yamlkit.ComputeMutations(previousParsedData, modifiedParsedData, functionIndex, resourceProvider)
	}
	return modifiedParsedData, mutations, err
}
