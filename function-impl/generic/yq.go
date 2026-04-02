// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package generic

import (
	"log/slog"

	"github.com/cockroachdb/errors"
	"github.com/confighub/sdk/core/configkit"
	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/configkit/yqkit"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/function/handler"
	"github.com/confighub/sdk/core/third_party/gaby"
)

func registerYQ(fh handler.FunctionRegistry, converter configkit.ConfigConverter, resourceProvider yamlkit.ResourceProvider) {
	if err := fh.RegisterFunction("yq", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "yq",
			Parameters: []api.FunctionParameter{
				{
					ParameterName: "yq-expression",
					Required:      true,
					Description:   "yq expression. Use `select() | ...` to get values from a subset of the documents.",
					DataType:      api.DataTypeString,
					Example:       ".spec.replicas",
				},
			},
			OutputInfo: &api.FunctionOutput{
				ResultName:  "yq output",
				Description: "Output from yq",
				OutputType:  api.OutputTypeYAML,
				Schema:      &api.YAMLPayloadSchema,
			},
			Mutating:              false,
			Validating:            false,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Returns the result of running yq with the specified expression on the configuration data",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
			return genericFnYQ(resourceProvider, fArgs.FunctionContext, fArgs.ParsedData, fArgs.Arguments, false)
		},
	}); err != nil {
		slog.Error("failed to register function", "error", err)
	}
}

func registerYQI(fh handler.FunctionRegistry, converter configkit.ConfigConverter, resourceProvider yamlkit.ResourceProvider) {
	if err := fh.RegisterFunction("yq-i", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "yq-i",
			Parameters: []api.FunctionParameter{
				{
					ParameterName: "yq-expression",
					Required:      true,
					Description:   "yq expression. Use `with(select(); ...)` to modify values in a subset of the documents.",
					DataType:      api.DataTypeString,
					Example:       ".spec.replicas = 7",
				},
			},
			Mutating:              true,
			Validating:            false,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "The configuration data is updated with the result of running yq -i with the specified expression on the configuration data",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
			return genericFnYQ(resourceProvider, fArgs.FunctionContext, fArgs.ParsedData, fArgs.Arguments, true)
		},
	}); err != nil {
		slog.Error("failed to register function", "error", err)
	}
}

func genericFnYQ(resourceProvider yamlkit.ResourceProvider, _ *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, mutating bool) (gaby.Container, any, error) {
	// The argument value types should be verified before this function is called
	expression := args[0].Value.(string)

	output, err := yqkit.EvalYQExpression(expression, parsedData.String())
	if err != nil {
		return parsedData, nil, errors.Wrap(err, "yq expression evaluation failed")
	}
	if mutating {
		parsedOutput, err := gaby.ParseAll([]byte(output))
		if err != nil {
			return parsedData, nil, errors.Wrap(err, "failed to parse output from yq")
		}
		return parsedOutput, nil, nil
	}
	wrappedOutput := api.YAMLPayload{Payload: output}
	return parsedData, wrappedOutput, nil
}
