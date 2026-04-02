// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package generic

import (
	"encoding/json"
	"log/slog"

	"github.com/confighub/sdk/core/configkit"
	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/function/handler"
	"github.com/confighub/sdk/core/third_party/gaby"
)

func registerNormalize(fh handler.FunctionRegistry, converter configkit.ConfigConverter, resourceProvider yamlkit.ResourceProvider) {
	if err := fh.RegisterFunction("normalize", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "normalize",
			Parameters: []api.FunctionParameter{
				{
					ParameterName:    "mutation-sources",
					Required:         false,
					Description:      "Previous mutation sources used to restore ResourceMergeIDs for resources that lost them",
					DataType:         api.DataTypeResourceMutationList,
					ValueConstraints: api.ValueConstraints{Schema: &api.ResourceMutationListSchema},
				},
			},
			Mutating:              true,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Assigns a unique ResourceID to each resource that doesn't already have one",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
			var mutationSources *api.ResourceMutationList
			if len(fArgs.Arguments) > 0 {
				var ms api.ResourceMutationList
				if err := json.Unmarshal([]byte(fArgs.Arguments[0].Value.(string)), &ms); err != nil {
					return fArgs.ParsedData, nil, err
				}
				mutationSources = &ms
			}
			err := yamlkit.Normalize(fArgs.ParsedData, resourceProvider, mutationSources)
			if err != nil {
				return fArgs.ParsedData, nil, err
			}
			return fArgs.ParsedData, nil, nil
		},
	}); err != nil {
		slog.Error("failed to register function", "error", err)
	}
}
