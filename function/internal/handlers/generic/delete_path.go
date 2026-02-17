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

func registerDeletePath(fh handler.FunctionRegistry, converter configkit.ConfigConverter, resourceProvider yamlkit.ResourceProvider) {
	fh.RegisterFunction("delete-path", &handler.FunctionRegistration{
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
		Function: func(functionContext *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument) (gaby.Container, any, error) {
			return GenericFnDeletePath(resourceProvider, functionContext, parsedData, args)
		},
	})
}

func GenericFnDeletePath(resourceProvider yamlkit.ResourceProvider, _ *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument) (gaby.Container, any, error) {
	// The argument value types should be verified before this function is called
	resourceType := args[0].Value.(string)
	unresolvedPath := args[1].Value.(string)

	resourceTypeToPaths := GetVisitorMapForPath(resourceProvider, api.ResourceType(resourceType), api.UnresolvedPath(unresolvedPath))
	err := yamlkit.DeletePaths(parsedData, resourceTypeToPaths, []any{}, resourceProvider)
	return parsedData, nil, err
}
