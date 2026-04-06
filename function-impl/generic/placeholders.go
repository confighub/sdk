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

func registerGetPlaceholders(fh handler.FunctionRegistry, converter configkit.ConfigConverter, resourceProvider yamlkit.ResourceProvider) {
	if err := fh.RegisterFunction("get-placeholders", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "get-placeholders",
			OutputInfo: &api.FunctionOutput{
				ResultName:  "path",
				Description: "Resource paths containing placeholder values",
				OutputType:  api.OutputTypeAttributeValueList,
				Schema:      &api.AttributeValueListSchema,
			},
			Mutating:              false,
			Validating:            false,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Returns a list of attributes containing the placeholder string 'confighubplaceholder' or number 999999999. See https://docs.confighub.com/background/concepts/placeholders/ for more information about placeholders.",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
			return genericFnGetPlaceholders(resourceProvider, fArgs.FunctionContext, fArgs.ParsedData, fArgs.Arguments, whereFromOptions(fArgs.Options))
		},
	}); err != nil {
		slog.Error("failed to register function", "error", err)
	}
}

func genericFnGetPlaceholders(resourceProvider yamlkit.ResourceProvider, _ *api.FunctionContext, parsedData gaby.Container, _ []api.FunctionArgument, whereExpressions []*api.VisitorRelationalExpression) (gaby.Container, any, error) {
	paths := yamlkit.FindYAMLPathsByValue(parsedData, resourceProvider, yamlkit.PlaceHolderBlockApplyString, whereExpressions)
	paths = append(paths, yamlkit.FindYAMLPathsByValue(parsedData, resourceProvider, yamlkit.PlaceHolderBlockApplyInt, whereExpressions)...)
	return parsedData, paths, nil
}

func registerVetPlaceholders(fh handler.FunctionRegistry, converter configkit.ConfigConverter, resourceProvider yamlkit.ResourceProvider) {
	if err := fh.RegisterFunction("vet-placeholders", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "vet-placeholders",
			OutputInfo: &api.FunctionOutput{
				ResultName:  "passed",
				Description: "True if no placeholders remain, false otherwise",
				OutputType:  api.OutputTypeValidationResult,
				Schema:      &api.ValidationResultListSchema,
			},
			Mutating:              false,
			Validating:            true,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Returns true if no attributes contain the placeholder string 'confighubplaceholder' or number 999999999. See https://docs.confighub.com/background/concepts/placeholders/ for more information about placeholders.",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
			return genericFnVetPlaceholders(resourceProvider, fArgs.FunctionContext, fArgs.ParsedData, fArgs.Arguments, whereFromOptions(fArgs.Options))
		},
	}); err != nil {
		slog.Error("failed to register function", "error", err)
	}
	// TODO: Deprecated in favor of vet-placeholders. Remove this.
	if err := fh.RegisterFunction("no-placeholders", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "no-placeholders",
			OutputInfo: &api.FunctionOutput{
				ResultName:  "passed",
				Description: "True if no placeholders remain, false otherwise",
				OutputType:  api.OutputTypeValidationResult,
				Schema:      &api.ValidationResultListSchema,
			},
			Mutating:              false,
			Validating:            true,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "[Deprecated; use vet-placeholders instead] Returns true if no attributes contain the placeholder string 'confighubplaceholder' or number 999999999. See https://docs.confighub.com/background/concepts/placeholders/ for more information about placeholders.",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
			return genericFnVetPlaceholders(resourceProvider, fArgs.FunctionContext, fArgs.ParsedData, fArgs.Arguments, whereFromOptions(fArgs.Options))
		},
	}); err != nil {
		slog.Error("failed to register function", "error", err)
	}
}

func registerGetPlaceholderMutations(fh handler.FunctionRegistry, converter configkit.ConfigConverter, resourceProvider yamlkit.ResourceProvider) {
	if err := fh.RegisterFunction("get-placeholder-mutations", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "get-placeholder-mutations",
			OutputInfo: &api.FunctionOutput{
				ResultName:  "mutations",
				Description: "Resource mutations for paths containing placeholder values",
				OutputType:  api.OutputTypeResourceMutationList,
				Schema:      &api.ResourceMutationListSchema,
			},
			Mutating:              false,
			Validating:            false,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Returns a list of resource mutations for attributes containing the placeholder string 'confighubplaceholder' or number 999999999. See https://docs.confighub.com/background/concepts/placeholders/ for more information about placeholders.",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
			return genericFnGetPlaceholderMutations(resourceProvider, fArgs.FunctionContext, fArgs.ParsedData, fArgs.Arguments, whereFromOptions(fArgs.Options))
		},
	}); err != nil {
		slog.Error("failed to register function", "error", err)
	}
}

func genericFnGetPlaceholderMutations(resourceProvider yamlkit.ResourceProvider, functionContext *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, whereExpressions []*api.VisitorRelationalExpression) (gaby.Container, any, error) {
	parsedData, placeholders, err := genericFnGetPlaceholders(resourceProvider, functionContext, parsedData, args, whereExpressions)
	if err != nil {
		return parsedData, nil, err
	}
	avl := placeholders.(api.AttributeValueList)
	mutations := api.AttributeValueListToResourceMutationList(avl)
	return parsedData, mutations, nil
}

func genericFnVetPlaceholders(resourceProvider yamlkit.ResourceProvider, _ *api.FunctionContext, parsedData gaby.Container, _ []api.FunctionArgument, whereExpressions []*api.VisitorRelationalExpression) (gaby.Container, any, error) {
	paths := yamlkit.FindYAMLPathsByValue(parsedData, resourceProvider, yamlkit.PlaceHolderBlockApplyString, whereExpressions)
	paths = append(paths, yamlkit.FindYAMLPathsByValue(parsedData, resourceProvider, yamlkit.PlaceHolderBlockApplyInt, whereExpressions)...)
	result := api.ValidationResult{
		Passed:           len(paths) == 0,
		FailedAttributes: paths,
	}
	return parsedData, result, nil
}
