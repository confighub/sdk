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

func registerDeletePath(fh handler.FunctionRegistry, converter configkit.ConfigConverter, resourceProvider yamlkit.ResourceProvider) {
	if err := fh.RegisterFunction("delete-path", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "delete-path",
			Parameters: []api.FunctionParameter{
				{
					ParameterName: "resource-type",
					Required:      true,
					Description:   "Resource type (" + resourceProvider.TypeDescription() + ") of the path to delete",
					DataType:      api.DataTypeString,
				},
				{
					ParameterName:    "path",
					Required:         true,
					Description:      "Dot-separated path to delete. See https://docs.confighub.com/guide/functions/#configuration-path-syntax for more details regarding path syntax.",
					DataType:         api.DataTypeString,
					ValueConstraints: api.ValueConstraints{Regexp: api.PathRegexpString},
				},
			},
			Mutating:              true,
			Validating:            false,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Deletes the specified attribute path",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
			return GenericFnDeletePath(resourceProvider, fArgs.FunctionContext, fArgs.ParsedData, fArgs.Arguments)
		},
	}); err != nil {
		slog.Error("failed to register function", "error", err)
	}
}

func GenericFnDeletePath(resourceProvider yamlkit.ResourceProvider, _ *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument) (gaby.Container, any, error) {
	// The argument value types should be verified before this function is called
	resourceType := args[0].Value.(string)
	unresolvedPath := args[1].Value.(string)

	resourceTypeToPaths := yamlkit.GetVisitorMapForPath(resourceProvider, api.ResourceType(resourceType), api.UnresolvedPath(unresolvedPath))
	err := yamlkit.DeletePaths(parsedData, resourceTypeToPaths, []any{}, resourceProvider, nil)
	return parsedData, nil, err
}
