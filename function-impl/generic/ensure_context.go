// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package generic

import (
	"log/slog"

	"github.com/confighub/sdk/core/configkit"
	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/constants"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/function/handler"
	"github.com/confighub/sdk/core/third_party/gaby"
)

// Deprecated: This functionality should be implemented in bridges going forward.
func registerEnsureContext(fh handler.FunctionRegistry, converter configkit.ConfigConverter, resourceProvider yamlkit.ResourceProvider) {
	if err := fh.RegisterFunction("ensure-context", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "ensure-context",
			Parameters: []api.FunctionParameter{
				{
					ParameterName: "add-context",
					Required:      true,
					Description:   "Context is set if true and removed if false",
					DataType:      api.DataTypeBool,
				},
			},
			Mutating:              true,
			Validating:            false,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "[Deprecated] It is recommended to bridge implementers to do this in the bridge instead so as not to clutter configuration data. Set function context values (e.g., unit slug, space ID) in configuration resource/element attributes (if possible) if add-context is true and remove the context if false. These values can be used to find the corresponding unit in ConfigHub, such as with `cub k8s source`.",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
			return genericFnEnsureContext(resourceProvider, fArgs.FunctionContext, fArgs.ParsedData, fArgs.Arguments, fArgs.Options)
		},
	}); err != nil {
		slog.Error("failed to register function", "error", err)
	}
}

// TODO: Remove this once the functionality moves into all bridges.
func genericFnEnsureContext(resourceProvider yamlkit.ResourceProvider, functionContext *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, options *api.FunctionOptions) (gaby.Container, any, error) {
	addContext := args[0].Value.(bool)

	// Check whether adding context is supported by the resource provider
	if resourceProvider.ContextPath(constants.UnitSlugKeySuffix) == "" {
		// Not supported, so just return
		return parsedData, nil, nil
	}

	visitor := func(doc *gaby.YamlDoc, output any, _ int, resourceInfo *api.ResourceInfo) (any, []error) {
		if addContext {
			_, err := doc.SetP(functionContext.UnitSlug, resourceProvider.ContextPath(constants.UnitSlugKeySuffix))
			if err != nil {
				return output, []error{err}
			}
			_, err = doc.SetP(functionContext.SpaceID.String(), resourceProvider.ContextPath(constants.SpaceIDKeySuffix))
			if err != nil {
				return output, []error{err}
			}
		} else {
			err := doc.DeleteP(resourceProvider.ContextPath(constants.UnitSlugKeySuffix))
			if err != nil {
				return output, []error{err}
			}
			err = doc.DeleteP(resourceProvider.ContextPath(constants.SpaceIDKeySuffix))
			if err != nil {
				return output, []error{err}
			}
			// Delete the RevisionNum regardless
			err = doc.DeleteP(resourceProvider.ContextPath(constants.RevisionNumKeySuffix))
			if err != nil {
				return output, []error{err}
			}
		}
		return output, nil
	}

	_, err := yamlkit.VisitResourcesFiltered(parsedData, nil, resourceProvider, options, visitor)
	return parsedData, nil, err
}
