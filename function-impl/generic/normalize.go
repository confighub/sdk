// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package generic

import (
	"github.com/confighub/sdk/core/configkit"
	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/function/handler"
	"github.com/confighub/sdk/core/third_party/gaby"
)

func registerNormalize(fh handler.FunctionRegistry, converter configkit.ConfigConverter, resourceProvider yamlkit.ResourceProvider) {
	fh.RegisterFunction("normalize", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName:          "normalize",
			Parameters:            []api.FunctionParameter{},
			Mutating:              true,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Assigns a unique ResourceID to each resource that doesn't already have one",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
			err := yamlkit.Normalize(fArgs.ParsedData, resourceProvider)
			if err != nil {
				return fArgs.ParsedData, nil, err
			}
			return fArgs.ParsedData, nil, nil
		},
	})
}
