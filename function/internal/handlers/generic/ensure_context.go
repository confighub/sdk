// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package generic

import (
	"fmt"

	"github.com/confighub/sdk/configkit"
	"github.com/confighub/sdk/configkit/yamlkit"
	"github.com/confighub/sdk/function/api"
	"github.com/confighub/sdk/function/handler"
	"github.com/confighub/sdk/third_party/gaby"
)

func registerEnsureContext(fh handler.FunctionRegistry, converter configkit.ConfigConverter, resourceProvider yamlkit.ResourceProvider) {
	fh.RegisterFunction("ensure-context", &handler.FunctionRegistration{
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
			Description:           "Set function context values (e.g., unit slug, space ID) in configuration resource/element attributes (if possible) if add-context is true and remove the context if false. These values can be used to find the corresponding unit in ConfigHub, such as with `cub k8s source`.",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(functionContext *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument) (gaby.Container, any, error) {
			return genericFnEnsureContext(resourceProvider, functionContext, parsedData, args)
		},
	})
}

func genericFnEnsureContext(resourceProvider yamlkit.ResourceProvider, functionContext *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument) (gaby.Container, any, error) {
	addContext := args[0].Value.(bool)

	// Check whether adding context is supported by the resource provider
	if resourceProvider.ContextPath("UnitSlug") == "" {
		// Not supported, so just return
		return parsedData, nil, nil
	}

	revisionNum := functionContext.RevisionNum

	// Currently changing the revision numbers in the config causes a lot of "revision spam"
	// https://github.com/confighubai/confighub/issues/2006
	// Also, what I would really want is setting the revision number in the pod template, but
	// only if the pod template otherwise changed, such as in the case of an image reference change,
	// so that the app could report what revision it was at.
	// https://github.com/confighubai/confighub/issues/1892
	// Do to the problem and lack of desired benefit, I'm disabling it for now.
	addRevisionNum := false

	if addContext {
		// If the data has changed, the revision will be incremented.
		newHash := api.HashConfigData([]byte(parsedData.String()))
		if newHash != functionContext.PreviousContentHash {
			revisionNum++
		}
	}

	for _, doc := range parsedData {
		if addContext {
			_, err := doc.SetP(functionContext.UnitSlug, resourceProvider.ContextPath("UnitSlug"))
			if err != nil {
				return parsedData, nil, err
			}
			_, err = doc.SetP(functionContext.SpaceID.String(), resourceProvider.ContextPath("SpaceID"))
			if err != nil {
				return parsedData, nil, err
			}
			if addRevisionNum {
				_, err = doc.SetP(fmt.Sprintf("%d", revisionNum), resourceProvider.ContextPath("RevisionNum"))
				if err != nil {
					return parsedData, nil, err
				}
			}
		} else {
			err := doc.DeleteP(resourceProvider.ContextPath("UnitSlug"))
			if err != nil {
				return parsedData, nil, err
			}
			err = doc.DeleteP(resourceProvider.ContextPath("SpaceID"))
			if err != nil {
				return parsedData, nil, err
			}
			// Delete the RevisionNum regardless
			err = doc.DeleteP(resourceProvider.ContextPath("RevisionNum"))
			if err != nil {
				return parsedData, nil, err
			}
		}
	}
	if addRevisionNum && addContext && revisionNum == functionContext.RevisionNum {
		// We may need to update the revision number if this function changed the data.
		newHash := api.HashConfigData([]byte(parsedData.String()))
		if newHash != functionContext.PreviousContentHash {
			revisionNum++
			for _, doc := range parsedData {
				_, err := doc.SetP(fmt.Sprintf("%d", revisionNum), resourceProvider.ContextPath("RevisionNum"))
				if err != nil {
					return parsedData, nil, err
				}
			}
		}
	}
	return parsedData, nil, nil
}
