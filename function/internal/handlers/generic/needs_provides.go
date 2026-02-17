// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package generic

import (
	"github.com/confighub/sdk/configkit"
	"github.com/confighub/sdk/configkit/yamlkit"
	"github.com/confighub/sdk/function/api"
	"github.com/confighub/sdk/function/handler"
	"github.com/confighub/sdk/third_party/gaby"
)

func registerGetNeeded(fh handler.FunctionRegistry, converter configkit.ConfigConverter, resourceProvider yamlkit.ResourceProvider) {
	fh.RegisterFunction("get-needed", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "get-needed",
			OutputInfo: &api.FunctionOutput{
				ResultName:  "attribute-list",
				Description: "Needed attributes",
				OutputType:  api.OutputTypeAttributeValueList,
				Schema:      &attributeValueListSchema,
			},
			Mutating:              false,
			Validating:            false,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Returns a list of needed attributes with setter functions. See https://docs.confighub.com/background/concepts/needsprovides/ for more information.",
			FunctionType:          api.FunctionTypePathVisitor,
			AttributeName:         api.AttributeNameNeededValue,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(functionContext *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument) (gaby.Container, any, error) {
			return genericFnGetNeeded(resourceProvider, functionContext, parsedData, args)
		},
	})
}

func genericFnGetNeeded(resourceProvider yamlkit.ResourceProvider, _ *api.FunctionContext, parsedData gaby.Container, _ []api.FunctionArgument) (gaby.Container, any, error) {
	values, err := yamlkit.GetRegisteredNeededStringPaths(parsedData, resourceProvider)
	// TODO: int, bool
	return parsedData, values, err
}

func registerGetProvided(fh handler.FunctionRegistry, converter configkit.ConfigConverter, resourceProvider yamlkit.ResourceProvider) {
	fh.RegisterFunction("get-provided", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "get-provided",
			OutputInfo: &api.FunctionOutput{
				ResultName:  "attribute-list",
				Description: "Provided attributes",
				OutputType:  api.OutputTypeAttributeValueList,
				Schema:      &attributeValueListSchema,
			},
			Mutating:              false,
			Validating:            false,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Returns a list of Provided attributes. See https://docs.confighub.com/background/concepts/needsprovides/ for more information.",
			FunctionType:          api.FunctionTypePathVisitor,
			AttributeName:         api.AttributeNameProvidedValue,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(functionContext *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument) (gaby.Container, any, error) {
			return genericFnGetProvided(resourceProvider, functionContext, parsedData, args)
		},
	})
}

func genericFnGetProvided(resourceProvider yamlkit.ResourceProvider, _ *api.FunctionContext, parsedData gaby.Container, _ []api.FunctionArgument) (gaby.Container, any, error) {
	values, err := yamlkit.GetRegisteredProvidedStringPaths(parsedData, resourceProvider)
	// TODO: int, bool
	return parsedData, values, err
}
