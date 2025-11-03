// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package properties

import (
	"github.com/confighub/sdk/configkit/propkit"
	"github.com/confighub/sdk/configkit/yamlkit"
	"github.com/confighub/sdk/function/api"
	"github.com/confighub/sdk/function/handler"
	"github.com/confighub/sdk/function/internal/handlers/generic"
	"github.com/confighub/sdk/third_party/gaby"
	"github.com/labstack/gommon/log"
	"github.com/swaggest/jsonschema-go"
)

// TODO: refactor to share code that's common across ToolchainTypes

func registerStandardFunctions(fh handler.FunctionRegistry) {
	generic.RegisterStandardFunctions(fh, propkit.PropertiesResourceProvider, propkit.PropertiesResourceProvider)

	reflector := jsonschema.Reflector{}
	validationResultListSchema, err := reflector.Reflect(api.ValidationResultList{})
	if err != nil {
		log.Errorf("couldn't get schema for api.ValidationResultList")
	}
	fh.RegisterFunction("validate", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "validate",
			OutputInfo: &api.FunctionOutput{
				ResultName:  "passed",
				Description: "True if schema passes validation, false otherwise",
				OutputType:  api.OutputTypeValidationResult,
				Schema:      &validationResultListSchema,
			},
			Mutating:              false,
			Validating:            true,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Returns true if schema passes validation. [Not implemented yet]",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: propFnValidate,
	})
}

// This is also defined in the bridge.
const NamespaceProperty = "configHub.kubernetes.namespace"

func initStandardFunctions() {
	// In general we don't recommend changing names of configs since names are used for identifying
	// configs across mutations, but it is necessary for "container" resources.
	var defaultNames = api.ResourceTypeToPathToVisitorInfoType{
		api.ResourceTypeAny: {
			api.UnresolvedPath(propkit.PropertiesResourceProvider.ScopelessResourceNamePath()): {
				Path:          api.UnresolvedPath(propkit.PropertiesResourceProvider.ScopelessResourceNamePath()),
				AttributeName: api.AttributeNameResourceName,
				DataType:      api.DataTypeString,
			},
		},
	}
	setterFunctionInvocation := &api.FunctionInvocation{
		FunctionName: "set-default-names",
	}
	for resourceType, pathInfos := range defaultNames {
		yamlkit.RegisterPathsByAttributeName(
			propkit.PropertiesResourceProvider,
			api.AttributeNameDefaultName,
			resourceType,
			pathInfos,
			nil,
			setterFunctionInvocation,
			false,
		)
		yamlkit.RegisterPathsByAttributeName(
			propkit.PropertiesResourceProvider,
			api.AttributeNameGeneral,
			resourceType,
			pathInfos,
			nil,
			setterFunctionInvocation,
			true,
		)
	}

	// TODO
	var attributePaths = api.ResourceTypeToPathToVisitorInfoType{}
	for resourceType, pathInfos := range attributePaths {
		yamlkit.RegisterPathsByAttributeName(
			propkit.PropertiesResourceProvider,
			api.AttributeNameGeneral,
			resourceType,
			pathInfos,
			nil,
			nil,
			true,
		)
	}

	// TODO
	var detailPaths = api.ResourceTypeToPathToVisitorInfoType{}
	for resourceType, pathInfos := range detailPaths {
		yamlkit.RegisterPathsByAttributeName(
			propkit.PropertiesResourceProvider,
			api.AttributeNameDetail,
			resourceType,
			pathInfos,
			nil,
			nil,
			false,
		)
	}

	path := api.UnresolvedPath(NamespaceProperty)
	pathInfos := api.PathToVisitorInfoType{
		path: {
			Path:          path,
			AttributeName: api.AttributeNameResourceName,
			DataType:      api.DataTypeString,
		},
	}
	// Function to set the value. The parameters are expected to match the corresponding
	// get function's parameters plus its result.
	setterFunctionInvocation = &api.FunctionInvocation{
		FunctionName: "set-references-of-type",
		Arguments:    []api.FunctionArgument{{ParameterName: "resource-type", Value: "v1/Namespace"}},
	}
	yamlkit.RegisterNeededPaths(propkit.PropertiesResourceProvider, api.ResourceTypeAny, pathInfos, setterFunctionInvocation)

}

func propFnValidate(_ *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, _ []byte) (gaby.Container, any, error) {
	// TODO
	return parsedData, api.ValidationResultTrue, nil
}
