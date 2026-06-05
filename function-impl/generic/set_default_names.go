// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package generic

import (
	"log/slog"

	"github.com/cockroachdb/errors"
	"github.com/confighub/sdk/core/configkit"
	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/function/handler"
	"github.com/confighub/sdk/core/third_party/gaby"
)

func registerSetDefaultNames(fh handler.FunctionRegistry, converter configkit.ConfigConverter, resourceProvider yamlkit.ResourceProvider) {
	if err := fh.RegisterFunction("set-default-names", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "set-default-names",
			Parameters: []api.FunctionParameter{
				{
					ParameterName: "name",
					Required:      true,
					Description:   "Name value to set in identified name fields containing the placeholder string 'confighubplaceholder' or number 999999999",
					DataType:      api.DataTypeString,
				},
			},
			Mutating:              true,
			Validating:            false,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Set the values of identified name fields containing the placeholder string 'confighubplaceholder' or number 999999999. See https://docs.confighub.com/background/concepts/placeholders/ for more details regarding placeholder values.",
			FunctionType:          api.FunctionTypePathVisitor,
			AttributeName:         api.AttributeNameDefaultName,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
			return genericFnSetDefaultNames(resourceProvider, fArgs.FunctionContext, fArgs.ParsedData, fArgs.Arguments, fArgs.Options)
		},
	}); err != nil {
		slog.Error("failed to register function", "error", err)
	}
}

func genericFnSetDefaultNames(resourceProvider yamlkit.ResourceProvider, functionContext *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, options *api.FunctionOptions) (gaby.Container, any, error) {
	nameValue := args[0].Value.(string)

	visitor := func(doc *gaby.YamlDoc, output any, context yamlkit.VisitorContext, currentValue string) (any, error) {
		if !yamlkit.IsStringPlaceHolderValue(currentValue) {
			return nil, nil
		}
		// Replace the placeholder string (and any named placeholders prefixed by
		// it) in place, preserving surrounding text so that distinct values such
		// as "confighubplaceholder-http" and "confighubplaceholder-https" remain
		// distinct after replacement.
		pathString := string(context.Path)
		newValue := yamlkit.ReplaceStringPlaceholder(currentValue, nameValue)
		_, err := doc.SetP(newValue, pathString)
		return nil, errors.Wrap(err, "unable to set value of "+pathString)
	}
	nameConstructors := yamlkit.GetPathRegistryForAttributeName(resourceProvider, api.AttributeNameDefaultName)
	_, err := yamlkit.VisitPaths[string](parsedData, nameConstructors, []any{}, nil, resourceProvider, visitor, false, options)
	return parsedData, nil, err
}
