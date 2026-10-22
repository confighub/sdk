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

func registerSetExplicitNulls(fh handler.FunctionRegistry, converter configkit.ConfigConverter, resourceProvider yamlkit.ResourceProvider) {
	if err := fh.RegisterFunction("set-explicit-nulls", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName:          "set-explicit-nulls",
			Mutating:              true,
			Validating:            false,
			Hermetic:              true,
			Idempotent:            true,
			Replayable:            true,
			Description:           "Writes null for every empty value (a key with nothing after it, as templates often render), the values vet-format reports as no-empty-values. The configuration means the same thing afterwards, since an empty value is already read as null.",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
			return genericFnSetExplicitNulls(resourceProvider, fArgs.Options, fArgs.ParsedData)
		},
	}); err != nil {
		slog.Error("failed to register function", "error", err)
	}
}

func genericFnSetExplicitNulls(resourceProvider yamlkit.ResourceProvider, options *api.FunctionOptions, parsedData gaby.Container) (gaby.Container, any, error) {
	_, err := yamlkit.VisitResourcesFiltered(parsedData, nil, resourceProvider, options,
		func(doc *gaby.YamlDoc, output any, index int, resourceInfo *api.ResourceInfo) (any, []error) {
			yamlkit.SetExplicitNulls(doc.YNode())
			return output, nil
		})
	return parsedData, nil, err
}
