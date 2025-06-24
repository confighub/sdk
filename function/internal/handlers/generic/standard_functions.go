// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package generic

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"text/template"

	"github.com/cockroachdb/errors"
	"github.com/cockroachdb/errors/join"
	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/labstack/gommon/log"
	"sigs.k8s.io/yaml"

	"github.com/confighub/sdk/configkit"
	"github.com/confighub/sdk/configkit/k8skit"
	"github.com/confighub/sdk/configkit/yamlkit"
	"github.com/confighub/sdk/function/api"
	"github.com/confighub/sdk/function/handler"
	"github.com/confighub/sdk/third_party/gaby"
)

func RegisterComputeMutations(fh handler.FunctionRegistry, converter configkit.ConfigConverter, resourceProvider yamlkit.ResourceProvider) {
	fh.RegisterFunction("compute-mutations", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "compute-mutations",
			Parameters: []api.FunctionParameter{
				{
					ParameterName: "config-doc-list",
					Required:      true,
					Description:   "Document list with the previous config data",
					DataType:      converter.DataType(),
				},
				{
					ParameterName: "functionIndex",
					Required:      true,
					Description:   "index of the function from the invocation list that mutated the config data",
					DataType:      api.DataTypeInt,
				},
				{
					ParameterName: "alreadyConverted",
					Required:      false,
					Description:   "if true, the config-doc-list is already converted to YAML",
					DataType:      api.DataTypeBool,
				},
			},
			OutputInfo: &api.FunctionOutput{
				ResultName:  "mutations",
				Description: "List of mutations in the same order as the resources in the config data",
				OutputType:  api.OutputTypeResourceMutationList,
			},
			Mutating:              false,
			Validating:            false,
			Hermetic:              true,
			Idempotent:            true,
			Description:           `Diffs the input with the config data and returns a list of mutations made to the config data`,
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(functionContext *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, liveState []byte) (gaby.Container, any, error) {
			return genericFnComputeMutations(converter, resourceProvider, functionContext, parsedData, args, liveState)
		},
	})
}

func RegisterStandardFunctions(fh handler.FunctionRegistry, converter configkit.ConfigConverter, resourceProvider yamlkit.ResourceProvider) {
	fh.RegisterFunction("get-resources", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "get-resources",
			OutputInfo: &api.FunctionOutput{
				ResultName:  "resource",
				Description: "Return the names, types, and bodies of the resources",
				OutputType:  api.OutputTypeResourceList,
			},
			Mutating:              false,
			Validating:            false,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Returns a list of resources and their types",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(functionContext *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, liveState []byte) (gaby.Container, any, error) {
			return genericFnGetResources(resourceProvider, functionContext, parsedData, args, liveState)
		},
	})
	fh.RegisterFunction("get-resources-of-type", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "get-resources-of-type",
			Parameters: []api.FunctionParameter{
				{
					ParameterName: "resource-type",
					Required:      true,
					Description:   "Type (" + resourceProvider.TypeDescription() + ") of the resources to return",
					DataType:      api.DataTypeString,
				},
			},
			OutputInfo: &api.FunctionOutput{
				ResultName:  "resource-name",
				Description: "Return the names of resources of the specified type",
				OutputType:  api.OutputTypeResourceInfoList,
			},
			Mutating:              false,
			Validating:            false,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Returns a list of resources of the specified type",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(functionContext *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, liveState []byte) (gaby.Container, any, error) {
			return genericFnGetResourcesOfType(resourceProvider, functionContext, parsedData, args, liveState)
		},
	})
	fh.RegisterFunction("set-references-of-type", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "set-references-of-type",
			Parameters: []api.FunctionParameter{
				{
					ParameterName: "resource-type",
					Required:      true,
					Description:   "Type (" + resourceProvider.TypeDescription() + ") of the config references to set",
					DataType:      api.DataTypeString,
				},
				{
					ParameterName: "resource-name",
					Required:      true,
					Description:   "Name to set in the resource references",
					DataType:      api.DataTypeString,
				},
			},
			Mutating:              true,
			Validating:            false,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Sets references targeting the specified type",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(functionContext *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, liveState []byte) (gaby.Container, any, error) {
			return genericFnSetReferencesOfType(resourceProvider, functionContext, parsedData, args, liveState)
		},
	})
	fh.RegisterFunction("get-placeholders", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "get-placeholders",
			OutputInfo: &api.FunctionOutput{
				ResultName:  "path",
				Description: "Resource paths containing placeholder values",
				OutputType:  api.OutputTypeAttributeValueList,
			},
			Mutating:              false,
			Validating:            false,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Returns a list of attributes containing the placeholder string 'replaceme' or number 999999999",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(functionContext *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, liveState []byte) (gaby.Container, any, error) {
			return genericFnGetPlaceholders(resourceProvider, functionContext, parsedData, args, liveState)
		},
	})
	fh.RegisterFunction("no-placeholders", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "no-placeholders",
			OutputInfo: &api.FunctionOutput{
				ResultName:  "passed",
				Description: "True if no placeholders remain, false otherwise",
				OutputType:  api.OutputTypeValidationResult,
			},
			Mutating:              false,
			Validating:            true,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Returns true if no attributes contain the placeholder string 'replaceme' or number 999999999",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(functionContext *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, liveState []byte) (gaby.Container, any, error) {
			return genericFnNoPlaceholders(resourceProvider, functionContext, parsedData, args, liveState)
		},
	})
	fh.RegisterFunction("get-string-path", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "get-string-path",
			Parameters: []api.FunctionParameter{
				{
					ParameterName: "resource-type",
					Required:      true,
					Description:   "Resource type (" + resourceProvider.TypeDescription() + ") of the attribute to get",
					DataType:      api.DataTypeString,
				},
				{
					ParameterName: "path",
					Required:      true,
					Description:   "Path whose value to get",
					DataType:      api.DataTypeString,
				},
			},
			OutputInfo: &api.FunctionOutput{
				ResultName:  "path",
				Description: "Value of the specified resource path",
				OutputType:  api.OutputTypeAttributeValueList,
			},
			Mutating:              false,
			Validating:            false,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Returns the value(s) of the specified attribute path",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(functionContext *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, liveState []byte) (gaby.Container, any, error) {
			return GenericFnGetStringPath(resourceProvider, functionContext, parsedData, args, liveState)
		},
	})
	fh.RegisterFunction("set-string-path", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "set-string-path",
			Parameters: []api.FunctionParameter{
				{
					ParameterName: "resource-type",
					Required:      true,
					Description:   "Resource type (" + resourceProvider.TypeDescription() + ") of the attribute to set",
					DataType:      api.DataTypeString,
				},
				{
					ParameterName: "path",
					Required:      true,
					Description:   "Path of the attribute to set",
					DataType:      api.DataTypeString,
				},
				{
					ParameterName: "attribute-value",
					Required:      true,
					Description:   "Value to set the attribute to",
					DataType:      api.DataTypeString,
				},
			},
			Mutating:              true,
			Validating:            false,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Set the value(s) of the specified attribute path",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(functionContext *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, liveState []byte) (gaby.Container, any, error) {
			return GenericFnSetStringPath(resourceProvider, functionContext, parsedData, args, liveState)
		},
	})
	fh.RegisterFunction("get-int-path", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "get-int-path",
			Parameters: []api.FunctionParameter{
				{
					ParameterName: "resource-type",
					Required:      true,
					Description:   "Resource type (" + resourceProvider.TypeDescription() + ") of the attribute to get",
					DataType:      api.DataTypeString,
				},
				{
					ParameterName: "path",
					Required:      true,
					Description:   "Path whose value to get",
					DataType:      api.DataTypeString,
				},
			},
			OutputInfo: &api.FunctionOutput{
				ResultName:  "path",
				Description: "Value of the specified resource path",
				OutputType:  api.OutputTypeAttributeValueList,
			},
			Mutating:              false,
			Validating:            false,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Returns the value(s) of the specified attribute path",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(functionContext *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, liveState []byte) (gaby.Container, any, error) {
			return GenericFnGetIntPath(resourceProvider, functionContext, parsedData, args, liveState)
		},
	})
	fh.RegisterFunction("set-int-path", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "set-int-path",
			Parameters: []api.FunctionParameter{
				{
					ParameterName: "resource-type",
					Required:      true,
					Description:   "Resource type (" + resourceProvider.TypeDescription() + ") of the attribute to set",
					DataType:      api.DataTypeString,
				},
				{
					ParameterName: "path",
					Required:      true,
					Description:   "Path of the attribute to set",
					DataType:      api.DataTypeString,
				},
				{
					ParameterName: "attribute-value",
					Required:      true,
					Description:   "Value to set the attribute to",
					DataType:      api.DataTypeInt,
				},
			},
			Mutating:              true,
			Validating:            false,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Set the value(s) of the specified attribute path",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(functionContext *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, liveState []byte) (gaby.Container, any, error) {
			return GenericFnSetIntPath(resourceProvider, functionContext, parsedData, args, liveState)
		},
	})
	fh.RegisterFunction("get-bool-path", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "get-bool-path",
			Parameters: []api.FunctionParameter{
				{
					ParameterName: "resource-type",
					Required:      true,
					Description:   "Resource type (" + resourceProvider.TypeDescription() + ") of the attribute to get",
					DataType:      api.DataTypeString,
				},
				{
					ParameterName: "path",
					Required:      true,
					Description:   "Path whose value to get",
					DataType:      api.DataTypeString,
				},
			},
			OutputInfo: &api.FunctionOutput{
				ResultName:  "path",
				Description: "Value of the specified resource path",
				OutputType:  api.OutputTypeAttributeValueList,
			},
			Mutating:              false,
			Validating:            false,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Returns the value(s) of the specified attribute path",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(functionContext *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, liveState []byte) (gaby.Container, any, error) {
			return GenericFnGetBoolPath(resourceProvider, functionContext, parsedData, args, liveState)
		},
	})
	fh.RegisterFunction("set-bool-path", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "set-bool-path",
			Parameters: []api.FunctionParameter{
				{
					ParameterName: "resource-type",
					Required:      true,
					Description:   "Resource type (" + resourceProvider.TypeDescription() + ") of the attribute to set",
					DataType:      api.DataTypeString,
				},
				{
					ParameterName: "path",
					Required:      true,
					Description:   "Path of the attribute to set",
					DataType:      api.DataTypeString,
				},
				{
					ParameterName: "attribute-value",
					Required:      true,
					Description:   "Value to set the attribute to",
					DataType:      api.DataTypeBool,
				},
			},
			Mutating:              true,
			Validating:            false,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Set the value(s) of the specified attribute path",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(functionContext *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, liveState []byte) (gaby.Container, any, error) {
			return GenericFnSetBoolPath(resourceProvider, functionContext, parsedData, args, liveState)
		},
	})
	fh.RegisterFunction("set-path-comment", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "set-path-comment",
			Parameters: []api.FunctionParameter{
				{
					ParameterName: "resource-type",
					Required:      true,
					Description:   "Resource type (" + resourceProvider.TypeDescription() + ") of the attribute to comment",
					DataType:      api.DataTypeString,
				},
				{
					ParameterName: "path",
					Required:      true,
					Description:   "Path of the attribute to comment",
					DataType:      api.DataTypeString,
				},
				{
					ParameterName: "comment",
					Required:      true,
					Description:   "Comment to attach to the attribute",
					DataType:      api.DataTypeString,
				},
			},
			Mutating:              true,
			Validating:            false,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Set the comment of the specified attribute path",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(functionContext *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, liveState []byte) (gaby.Container, any, error) {
			return genericFnSetPathComment(resourceProvider, functionContext, parsedData, args, liveState)
		},
	})
	fh.RegisterFunction("set-default-names", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName:          "set-default-names",
			Mutating:              true,
			Validating:            false,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Set identifying/uniquifying names to default patterns",
			FunctionType:          api.FunctionTypeCustom,
			AttributeName:         api.AttributeNameDefaultName,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(functionContext *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, liveState []byte) (gaby.Container, any, error) {
			return genericFnSetDefaultNames(resourceProvider, functionContext, parsedData, args, liveState)
		},
	})
	fh.RegisterFunction("get-attributes", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "get-attributes",
			OutputInfo: &api.FunctionOutput{
				ResultName:  "attribute",
				Description: "Significant attributes of common resource types",
				OutputType:  api.OutputTypeAttributeValueList,
			},
			Mutating:              false,
			Validating:            false,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Returns a list of significant attributes",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(functionContext *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, liveState []byte) (gaby.Container, any, error) {
			return genericFnGetAttributes(resourceProvider, functionContext, parsedData, args, liveState)
		},
	})
	fh.RegisterFunction("set-attributes", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "set-attributes",
			Parameters: []api.FunctionParameter{
				{
					ParameterName: "attribute-list",
					Required:      true,
					Description:   "List of attributes to set",
					DataType:      api.DataTypeAttributeValueList,
				},
			},
			Mutating:              true,
			Validating:            false,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Set specified attributes",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(functionContext *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, liveState []byte) (gaby.Container, any, error) {
			return genericFnSetAttributes(resourceProvider, functionContext, parsedData, args, liveState)
		},
	})
	fh.RegisterFunction("get-needed", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "get-needed",
			OutputInfo: &api.FunctionOutput{
				ResultName:  "attribute",
				Description: "Needed attributes",
				OutputType:  api.OutputTypeAttributeValueList,
			},
			Mutating:              false,
			Validating:            false,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Returns a list of needed attributes with setter functions",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(functionContext *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, liveState []byte) (gaby.Container, any, error) {
			return genericFnGetNeeded(resourceProvider, functionContext, parsedData, args, liveState)
		},
	})
	fh.RegisterFunction("get-provided", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "get-provided",
			OutputInfo: &api.FunctionOutput{
				ResultName:  "attribute",
				Description: "Provided attributes",
				OutputType:  api.OutputTypeAttributeValueList,
			},
			Mutating:              false,
			Validating:            false,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Returns a list of Provided attributes",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(functionContext *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, liveState []byte) (gaby.Container, any, error) {
			return genericFnGetProvided(resourceProvider, functionContext, parsedData, args, liveState)
		},
	})
	fh.RegisterFunction("cel-validate", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "cel-validate",
			Parameters: []api.FunctionParameter{
				{
					ParameterName: "validation-expr",
					Required:      true,
					Description:   "CEL expression to validate each resource",
					DataType:      api.DataTypeCEL,
				},
			},
			OutputInfo: &api.FunctionOutput{
				ResultName:  "passed",
				Description: "True if validation passed, false otherwise",
				OutputType:  api.OutputTypeValidationResult,
			},
			Mutating:              false,
			Validating:            true,
			Hermetic:              true,
			Idempotent:            true,
			Description:           `Returns true if validation expression evaluates to true for all resources`,
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(functionContext *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, liveState []byte) (gaby.Container, any, error) {
			return genericFnCELValidate(resourceProvider, functionContext, parsedData, args, liveState)
		},
	})
	fh.RegisterFunction("where-filter", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "where-filter",
			Parameters: []api.FunctionParameter{
				{
					ParameterName: "resource-type",
					Required:      true,
					Description:   "Resource type (" + resourceProvider.TypeDescription() + ") to match",
					DataType:      api.DataTypeString,
				},
				{
					ParameterName: "where-expression",
					Required:      true,
					Description:   "Where filter: The specified string is an expression for the purpose of evaluating whether the configuration data matches the filter. The expression syntax was inspired by SQL. It supports conjunctions using `AND` of relational expressions of the form *path* *operator* *literal*. The path specifications are dot-separated, for both map fields and array indices, as in `spec.template.spec.containers.0.image = 'ghcr.io/headlamp-k8s/headlamp:latest' AND spec.replicas > 1`. Strings and integers support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`. Boolean values support equality and inequality only. String literals are quoted with single quotes, such as `'string'`. Integer and boolean literals are also supported for attributes of those types.",
					DataType:      api.DataTypeString,
				},
			},
			OutputInfo: &api.FunctionOutput{
				ResultName:  "matched",
				Description: "True if filter passed for at least one resource, false otherwise",
				OutputType:  api.OutputTypeValidationResult,
			},
			Mutating:              false,
			Validating:            true,
			Hermetic:              true,
			Idempotent:            true,
			Description:           `Returns true if all terms of the conjunction of relational expressions evaluate to true for at least one matching path of a resource of the specified type`,
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(functionContext *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, liveState []byte) (gaby.Container, any, error) {
			return genericFnResourceWhereMatch(resourceProvider, functionContext, parsedData, args, liveState)
		},
	})
	fh.RegisterFunction("yq", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "yq",
			Parameters: []api.FunctionParameter{
				{
					ParameterName: "yq-expression",
					Required:      true,
					Description:   "yq expression",
					DataType:      api.DataTypeString,
				},
			},
			OutputInfo: &api.FunctionOutput{
				ResultName:  "yq output",
				Description: "Output from yq",
				OutputType:  api.OutputTypeYAML,
			},
			Mutating:              false,
			Validating:            false,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Returns the result of running yq with the specified expression on the YAML configuration data",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(functionContext *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, liveState []byte) (gaby.Container, any, error) {
			return genericFnYQ(resourceProvider, functionContext, parsedData, args, liveState)
		},
	})
	fh.RegisterFunction("is-approved", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "is-approved",
			Parameters: []api.FunctionParameter{
				{
					ParameterName: "num-approvers",
					Required:      true,
					Description:   "Number of approvers",
					DataType:      api.DataTypeInt,
				},
			},
			OutputInfo: &api.FunctionOutput{
				ResultName:  "passed",
				Description: "True if approvers are present, false otherwise",
				OutputType:  api.OutputTypeValidationResult,
			},
			Mutating:              false,
			Validating:            true,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Returns true if sufficient approvers are present",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(functionContext *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, liveState []byte) (gaby.Container, any, error) {
			return genericFnIsApproved(resourceProvider, functionContext, parsedData, args, liveState)
		},
	})
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
			Description:           "Set function context values in configuration resource/element attributes (if possible) if addContext is true and remove the context if false",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(functionContext *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, liveState []byte) (gaby.Container, any, error) {
			return genericFnEnsureContext(resourceProvider, functionContext, parsedData, args, liveState)
		},
	})

	fh.RegisterFunction("get-details", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "get-details",
			OutputInfo: &api.FunctionOutput{
				ResultName:  "attribute",
				Description: "Selected significant resource attributes",
				OutputType:  api.OutputTypeAttributeValueList,
			},
			Mutating:              false,
			Validating:            false,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Returns a list of selected significant resource attributes",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(functionContext *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, liveState []byte) (gaby.Container, any, error) {
			return genericFnGetDetails(resourceProvider, functionContext, parsedData, args, liveState)
		},
	})

	fh.RegisterFunction("upsert-resource", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "upsert-resource",
			Parameters: []api.FunctionParameter{
				{
					ParameterName: "resource-list",
					Required:      true,
					Description:   "ResourceList containing the resource to upsert",
					DataType:      api.DataTypeResourceList,
				},
				{
					ParameterName: "resource-type",
					Required:      true,
					Description:   "Type (" + resourceProvider.TypeDescription() + ") of the resource to upsert",
					DataType:      api.DataTypeString,
				},
				{
					ParameterName: "resource-name",
					Required:      true,
					Description:   "Name of the resource to upsert",
					DataType:      api.DataTypeString,
				},
			},
			Mutating:              true,
			Validating:            false,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Append the resource if it is not present or replace the existing resource if it is already present in the configuration data",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(functionContext *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, liveState []byte) (gaby.Container, any, error) {
			return genericFnUpsertResource(resourceProvider, functionContext, parsedData, args, liveState)
		},
	})

	fh.RegisterFunction("delete-resource", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "delete-resource",
			Parameters: []api.FunctionParameter{
				{
					ParameterName: "resource-type",
					Required:      true,
					Description:   "Type (" + resourceProvider.TypeDescription() + ") of the resource to delete",
					DataType:      api.DataTypeString,
				},
				{
					ParameterName: "resource-name",
					Required:      true,
					Description:   "Name of the resource to delete",
					DataType:      api.DataTypeString,
				},
			},
			Mutating:              true,
			Validating:            false,
			Hermetic:              true,
			Idempotent:            false,
			Description:           "Remove the specified resource from the configuration data",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(functionContext *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, liveState []byte) (gaby.Container, any, error) {
			return genericFnDeleteResource(resourceProvider, functionContext, parsedData, args, liveState)
		},
	})

	RegisterComputeMutations(fh, converter, resourceProvider)

	fh.RegisterFunction("patch-mutations", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "patch-mutations",
			Parameters: []api.FunctionParameter{
				{
					ParameterName: "mutation-predicates",
					Required:      true,
					Description:   "Mutations with predicates set to true if they are patchable",
					DataType:      api.DataTypeResourceMutationList,
				},
				{
					ParameterName: "mutation-patch",
					Required:      true,
					Description:   "Mutations to filter and patch",
					DataType:      api.DataTypeResourceMutationList,
				},
			},
			Mutating:              true,
			Validating:            false,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Selectively patch attributes if their mutations indicate they are patchable",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(functionContext *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, liveState []byte) (gaby.Container, any, error) {
			return genericFnPatchMutations(resourceProvider, functionContext, parsedData, args, liveState)
		},
	})
	fh.RegisterFunction("reset", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "reset",
			Parameters: []api.FunctionParameter{
				{
					ParameterName: "mutation-predicates",
					Required:      true,
					Description:   "Mutations with predicates set to true if they should be reset",
					DataType:      api.DataTypeResourceMutationList,
				},
			},
			Mutating:              true,
			Validating:            false,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Sets attributes back to placeholder values if last set by mutations that match the predicates",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(functionContext *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, liveState []byte) (gaby.Container, any, error) {
			return genericFnReset(resourceProvider, functionContext, parsedData, args, liveState)
		},
	})
	fh.RegisterFunction("replicate", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "replicate",
			Parameters: []api.FunctionParameter{
				{
					ParameterName: "resource-type",
					Required:      true,
					Description:   "Type (" + resourceProvider.TypeDescription() + ") of the resource/element to replicate",
					DataType:      api.DataTypeString,
				},
				{
					ParameterName: "resource-name",
					Required:      true,
					Description:   "Name of the resource/element to replicate",
					DataType:      api.DataTypeString,
				},
				{
					ParameterName: "replicas",
					Required:      true,
					Description:   "Desired number of replicas of the resource/element",
					DataType:      api.DataTypeInt,
				},
				{
					ParameterName: "resource-category",
					Required:      false,
					Description:   "Category of the resource/element to replicate",
					DataType:      api.DataTypeString,
				},
			},
			Mutating:              true,
			Validating:            false,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Replicate the specified configuration resource/element replicas-1 times",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(functionContext *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, liveState []byte) (gaby.Container, any, error) {
			return genericFnReplicate(resourceProvider, functionContext, parsedData, args, liveState)
		},
	})
}

func attributeNameForResourceType(resourceType api.ResourceType) api.AttributeName {
	return api.AttributeName(string(api.AttributeNameResourceName) + "/" + string(resourceType))
}

func genericFnGetResources(resourceProvider yamlkit.ResourceProvider, _ *api.FunctionContext, parsedData gaby.Container, _ []api.FunctionArgument, _ []byte) (gaby.Container, any, error) {
	list := make(api.ResourceList, 0, len(parsedData))
	for _, doc := range parsedData {
		resourceCategory, err := resourceProvider.ResourceCategoryGetter(doc)
		if err != nil {
			return parsedData, nil, err
		}
		resourceType, err := resourceProvider.ResourceTypeGetter(doc)
		if err != nil {
			return parsedData, nil, err
		}
		resourceName, err := resourceProvider.ResourceNameGetter(doc)
		if err != nil {
			return parsedData, nil, err
		}
		list = append(list, api.Resource{
			ResourceInfo: api.ResourceInfo{
				ResourceName:             resourceName,
				ResourceNameWithoutScope: resourceProvider.RemoveScopeFromResourceName(resourceName),
				ResourceType:             resourceType,
				ResourceCategory:         resourceCategory,
			},
			ResourceBody: doc.String(),
		})
	}
	return parsedData, list, nil
}

func genericFnGetResourcesOfType(resourceProvider yamlkit.ResourceProvider, _ *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, _ []byte) (gaby.Container, any, error) {
	resourceType := args[0].Value.(string)
	resourceMap, _, err := yamlkit.ResourceAndCategoryTypeMaps(parsedData, resourceProvider)
	if err != nil {
		return parsedData, nil, err
	}
	list := make(api.ResourceInfoList, 0, len(resourceMap))
	for resname, resCategoryTypes := range resourceMap {
		for _, resCategoryType := range resCategoryTypes {
			if resCategoryType.ResourceType == api.ResourceType(resourceType) {
				list = append(list, api.ResourceInfo{
					ResourceName:             resname,
					ResourceNameWithoutScope: resourceProvider.RemoveScopeFromResourceName(resname),
					ResourceType:             resCategoryType.ResourceType,
					ResourceCategory:         resCategoryType.ResourceCategory,
				})
			}
		}
	}
	return parsedData, list, nil
}

func genericFnSetReferencesOfType(resourceProvider yamlkit.ResourceProvider, _ *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, _ []byte) (gaby.Container, any, error) {
	resourceType := args[0].Value.(string)
	resourceName := args[1].Value.(string)

	var err error
	paths := yamlkit.GetPathRegistryForAttributeName(resourceProvider, attributeNameForResourceType(api.ResourceType(resourceType)))
	if paths != nil {
		err = yamlkit.UpdateStringPaths(parsedData, paths, []any{}, resourceProvider, resourceName)
	}
	return parsedData, nil, err
}

func genericFnGetPlaceholders(resourceProvider yamlkit.ResourceProvider, _ *api.FunctionContext, parsedData gaby.Container, _ []api.FunctionArgument, _ []byte) (gaby.Container, any, error) {
	paths := yamlkit.FindYAMLPathsByValue(parsedData, resourceProvider, yamlkit.PlaceHolderBlockApplyString)
	// OriginalName annotations can contain replaceme values for namespaces and/or names.
	// Ignore those. They aren't a problem for apply.
	filteredPaths := make(api.AttributeValueList, 0, len(paths))
	for _, pathValue := range paths {
		filteredPaths = append(filteredPaths, pathValue)
	}
	paths = append(filteredPaths, yamlkit.FindYAMLPathsByValue(parsedData, resourceProvider, yamlkit.PlaceHolderBlockApplyInt)...)
	return parsedData, paths, nil
}

func genericFnNoPlaceholders(resourceProvider yamlkit.ResourceProvider, _ *api.FunctionContext, parsedData gaby.Container, _ []api.FunctionArgument, _ []byte) (gaby.Container, any, error) {
	paths := yamlkit.FindYAMLPathsByValue(parsedData, resourceProvider, yamlkit.PlaceHolderBlockApplyString)
	paths = append(paths, yamlkit.FindYAMLPathsByValue(parsedData, resourceProvider, yamlkit.PlaceHolderBlockApplyInt)...)
	// OriginalName annotations can contain replaceme values for namespaces and/or names.
	// Ignore those. They aren't a problem for apply.
	filteredPaths := make(api.AttributeValueList, 0, len(paths))
	for _, pathValue := range paths {
		filteredPaths = append(filteredPaths, pathValue)
	}
	result := api.ValidationResult{
		Passed: len(filteredPaths) == 0,
	}
	return parsedData, result, nil
}

// GetVisitorMapForPath is used to get visitor info for a resolved path.
func GetVisitorMapForPath(resourceProvider yamlkit.ResourceProvider, rt api.ResourceType, path api.UnresolvedPath) api.ResourceTypeToPathToVisitorInfoType {
	visitorInfo := yamlkit.GetPathVisitorInfo(resourceProvider, rt, path)
	if visitorInfo == nil {
		visitorInfo = &api.PathVisitorInfo{}
		visitorInfo.AttributeName = api.AttributeNameGeneral
		visitorInfo.Path = path
	} else {
		// Create a copy to modify
		specificVisitorInfo := *visitorInfo
		visitorInfo = &specificVisitorInfo
		// Path may be overridden below
	}
	if yamlkit.PathIsResolved(string(path)) {
		visitorInfo.ResolvedPath = api.ResolvedPath(path)
	} else {
		visitorInfo.Path = path
	}
	resourceTypeToPaths := api.ResourceTypeToPathToVisitorInfoType{
		rt: {path: visitorInfo},
	}
	return resourceTypeToPaths
}

func GenericFnGetStringPath(resourceProvider yamlkit.ResourceProvider, _ *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, _ []byte) (gaby.Container, any, error) {
	// The argument value types should be verified before this function is called
	resourceType := args[0].Value.(string)
	unresolvedPath := args[1].Value.(string)

	resourceTypeToPaths := GetVisitorMapForPath(resourceProvider, api.ResourceType(resourceType), api.UnresolvedPath(unresolvedPath))
	values, err := yamlkit.GetStringPaths(parsedData, resourceTypeToPaths, []any{}, resourceProvider)
	return parsedData, values, err
}

func GenericFnSetStringPath(resourceProvider yamlkit.ResourceProvider, _ *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, _ []byte) (gaby.Container, any, error) {
	// The argument value types should be verified before this function is called
	resourceType := args[0].Value.(string)
	unresolvedPath := args[1].Value.(string)
	value := args[2].Value.(string)

	resourceTypeToPaths := GetVisitorMapForPath(resourceProvider, api.ResourceType(resourceType), api.UnresolvedPath(unresolvedPath))
	err := yamlkit.UpdateStringPaths(parsedData, resourceTypeToPaths, []any{}, resourceProvider, value)
	return parsedData, nil, err
}

func GenericFnGetIntPath(resourceProvider yamlkit.ResourceProvider, _ *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, _ []byte) (gaby.Container, any, error) {
	// The argument value types should be verified before this function is called
	resourceType := args[0].Value.(string)
	unresolvedPath := args[1].Value.(string)

	resourceTypeToPaths := GetVisitorMapForPath(resourceProvider, api.ResourceType(resourceType), api.UnresolvedPath(unresolvedPath))
	values, err := yamlkit.GetPaths[int](parsedData, resourceTypeToPaths, []any{}, resourceProvider)
	return parsedData, values, err
}

func GenericFnSetIntPath(resourceProvider yamlkit.ResourceProvider, _ *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, _ []byte) (gaby.Container, any, error) {
	// The argument value types should be verified before this function is called
	resourceType := args[0].Value.(string)
	unresolvedPath := args[1].Value.(string)
	value := args[2].Value.(int)

	resourceTypeToPaths := GetVisitorMapForPath(resourceProvider, api.ResourceType(resourceType), api.UnresolvedPath(unresolvedPath))
	err := yamlkit.UpdatePathsValue[int](parsedData, resourceTypeToPaths, []any{}, resourceProvider, value)
	return parsedData, nil, err
}

func GenericFnGetBoolPath(resourceProvider yamlkit.ResourceProvider, _ *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, _ []byte) (gaby.Container, any, error) {
	// The argument value types should be verified before this function is called
	resourceType := args[0].Value.(string)
	unresolvedPath := args[1].Value.(string)

	resourceTypeToPaths := GetVisitorMapForPath(resourceProvider, api.ResourceType(resourceType), api.UnresolvedPath(unresolvedPath))
	values, err := yamlkit.GetPaths[bool](parsedData, resourceTypeToPaths, []any{}, resourceProvider)
	return parsedData, values, err
}

func GenericFnSetBoolPath(resourceProvider yamlkit.ResourceProvider, _ *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, _ []byte) (gaby.Container, any, error) {
	// The argument value types should be verified before this function is called
	resourceType := args[0].Value.(string)
	unresolvedPath := args[1].Value.(string)
	value := args[2].Value.(bool)

	resourceTypeToPaths := GetVisitorMapForPath(resourceProvider, api.ResourceType(resourceType), api.UnresolvedPath(unresolvedPath))
	err := yamlkit.UpdatePathsValue[bool](parsedData, resourceTypeToPaths, []any{}, resourceProvider, value)
	return parsedData, nil, err
}

func genericFnSetPathComment(resourceProvider yamlkit.ResourceProvider, _ *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, _ []byte) (gaby.Container, any, error) {
	// The argument value types should be verified before this function is called
	resourceType := args[0].Value.(string)
	unresolvedPath := args[1].Value.(string)
	comment := args[2].Value.(string)

	resourceTypeToPaths := GetVisitorMapForPath(resourceProvider, api.ResourceType(resourceType), api.UnresolvedPath(unresolvedPath))
	visitor := func(doc *gaby.YamlDoc, output any, _ yamlkit.VisitorContext, currentDoc *gaby.YamlDoc) (any, error) {
		currentDoc.SetComment(comment)
		return output, nil
	}
	_, err := yamlkit.VisitPathsDoc(parsedData, resourceTypeToPaths, []any{}, nil, resourceProvider, visitor)
	return parsedData, nil, err
}

type NameConstructorArgs struct {
	NormalizedUnitName     string
	NormalizedSpaceName    string
	NormalizedResourceName string
	TrimmedResourceName    string
	NormalizedResourceType string
}

const (
	StandardNamePrefixTemplate1 = "{{.NormalizedUnitName}}"
	StandardNamePrefixTemplate2 = "{{.NormalizedSpaceName}}"
)

func StandardNameTemplate(separator string) string {
	return StandardNamePrefixTemplate1 + separator + StandardNamePrefixTemplate2
}

func trimResourceName(resourceName, typeName, spaceName, unitName, separator string) string {
	// The type may be used as a suffix
	name := strings.TrimSuffix(strings.TrimSuffix(resourceName, typeName), separator)
	// The unit and space may be used as prefixes
	name = strings.TrimPrefix(strings.TrimPrefix(name, unitName), separator)
	name = strings.TrimPrefix(strings.TrimPrefix(name, spaceName), separator)
	return name
}

func genericFnSetDefaultNames(resourceProvider yamlkit.ResourceProvider, functionContext *api.FunctionContext, parsedData gaby.Container, _ []api.FunctionArgument, _ []byte) (gaby.Container, any, error) {
	visitor := func(doc *gaby.YamlDoc, output any, context yamlkit.VisitorContext, currentValue string) (any, error) {
		if !strings.Contains(currentValue, yamlkit.PlaceHolderBlockApplyString) {
			return nil, nil
		}
		nameTemplate := context.Info.GenerationTemplate
		if nameTemplate == "" {
			log.Errorf("no name constructor template: %v", context.Info)
			return nil, errors.New("internal error") // TODO: create error type
		}
		unitName := resourceProvider.NormalizeName(functionContext.UnitSlug)
		spaceName := resourceProvider.NormalizeName(functionContext.SpaceSlug)
		resourceName := resourceProvider.NormalizeName(string(context.ResourceName))
		resourceType := resourceProvider.NormalizeName(string(context.ResourceType))
		f := template.FuncMap{}
		f["toUpper"] = strings.ToUpper
		f["toLower"] = strings.ToLower
		f["toUpper"] = strings.ToUpper
		f["trimSpace"] = strings.TrimSpace
		f["trimSuffix"] = strings.TrimSuffix
		f["trimPrefix"] = strings.TrimPrefix
		tmpl, err := template.New("name").Funcs(f).Parse(nameTemplate)
		if err != nil {
			log.Errorf("couldn't parse template %s: %v", nameTemplate, err)
			return nil, errors.New("internal error") // TODO: create an error type
		}
		constructorArgs := NameConstructorArgs{
			unitName,
			spaceName,
			resourceName,
			trimResourceName(resourceName, resourceType, spaceName, unitName, resourceProvider.NameSeparator()),
			resourceType,
		}
		var out bytes.Buffer
		err = tmpl.Execute(&out, constructorArgs)
		if err != nil {
			log.Errorf("error evaluating template: %v", err)
			return nil, errors.New("internal error") // TODO: create an error type
		} else {
			defaultName := out.String()
			// We can't replace the placeholder string because reset doesn't restore the original
			// string, it replaces the whole field with the placeholder value. The whole new value
			// for each specific field is expected to be generated by the default name template.
			// Once the template strings are made extensible via API they will be easier to customize.
			// newValue := strings.ReplaceAll(currentValue, yamlkit.PlaceHolderBlockApplyString, defaultName)
			newValue := defaultName
			_, err = doc.SetP(newValue, string(context.Path))
			return nil, errors.WithStack(err) // TODO: wrap error?
		}
	}
	nameConstructors := yamlkit.GetPathRegistryForAttributeName(resourceProvider, api.AttributeNameDefaultName)
	_, err := yamlkit.VisitPaths[string](parsedData, nameConstructors, []any{}, nil, resourceProvider, visitor)
	return parsedData, nil, err
}

func genericFnGetAttributes(resourceProvider yamlkit.ResourceProvider, _ *api.FunctionContext, parsedData gaby.Container, _ []api.FunctionArgument, _ []byte) (gaby.Container, any, error) {
	attributePaths := yamlkit.GetPathRegistryForAttributeName(resourceProvider, api.AttributeNameGeneral)
	values, err := yamlkit.GetPathsAnyType(parsedData, attributePaths, []any{}, resourceProvider, api.DataTypeNone, false)
	return parsedData, values, err
}

func genericFnSetAttributes(resourceProvider yamlkit.ResourceProvider, functionContext *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, liveState []byte) (gaby.Container, any, error) {
	attributeListString := args[0].Value.(string)
	var attributeList api.AttributeValueList
	err := json.Unmarshal([]byte(attributeListString), &attributeList)
	if err != nil {
		return parsedData, nil, err
	}
	var multiErrs []error
	for _, attribute := range attributeList {
		setterArgs := make([]api.FunctionArgument, 3)
		// TODO: match resourceName if set?
		setterArgs[0].Value = string(attribute.ResourceType)
		setterArgs[1].Value = string(attribute.Path)
		switch attribute.DataType {
		case api.DataTypeString:
			stringValue, ok := attribute.Value.(string)
			if !ok {
				multiErrs = append(multiErrs, fmt.Errorf("value of attribute %s is not string: %v", attribute.AttributeName, attribute.Value))
			} else {
				setterArgs[2].Value = stringValue
				parsedData, _, err = GenericFnSetStringPath(resourceProvider, functionContext, parsedData, setterArgs, liveState)
				if err != nil {
					multiErrs = append(multiErrs, err)
				}
			}
		case api.DataTypeInt:
			// Integers parse as float64
			floatValue, ok := attribute.Value.(float64)
			if !ok {
				multiErrs = append(multiErrs, fmt.Errorf("value of attribute %s is not int: %v", attribute.AttributeName, attribute.Value))
			} else {
				intValue := int(math.Round(floatValue))
				setterArgs[2].Value = intValue
				parsedData, _, err = GenericFnSetIntPath(resourceProvider, functionContext, parsedData, setterArgs, liveState)
				if err != nil {
					multiErrs = append(multiErrs, err)
				}
			}
		case api.DataTypeBool:
			boolValue, ok := attribute.Value.(bool)
			if !ok {
				multiErrs = append(multiErrs, fmt.Errorf("value of attribute %s is not bool: %v", attribute.AttributeName, attribute.Value))
			} else {
				setterArgs[2].Value = boolValue
				parsedData, _, err = GenericFnSetBoolPath(resourceProvider, functionContext, parsedData, setterArgs, liveState)
				if err != nil {
					multiErrs = append(multiErrs, err)
				}
			}
		default:
			multiErrs = append(multiErrs, fmt.Errorf("unsupported data type %s", attribute.DataType))
		}
	}
	if len(multiErrs) != 0 {
		return parsedData, nil, join.Join(multiErrs...)
	}
	return parsedData, nil, nil
}

func genericFnGetNeeded(resourceProvider yamlkit.ResourceProvider, _ *api.FunctionContext, parsedData gaby.Container, _ []api.FunctionArgument, _ []byte) (gaby.Container, any, error) {
	values, err := yamlkit.GetRegisteredNeededStringPaths(parsedData, resourceProvider)
	// TODO: int, bool
	return parsedData, values, err
}

func genericFnGetProvided(resourceProvider yamlkit.ResourceProvider, _ *api.FunctionContext, parsedData gaby.Container, _ []api.FunctionArgument, liveState []byte) (gaby.Container, any, error) {
	values, err := yamlkit.GetRegisteredProvidedStringPaths(parsedData, resourceProvider)
	if err != nil {
		return parsedData, values, err
	}
	// TODO: int, bool
	// TODO: handle multiple different possible liveState formats for different providers
	// For now, this assumes Kubernetes resources
	if len(liveState) != 0 {
		parsedLiveState, err := gaby.ParseAll(liveState)
		if err != nil {
			return parsedData, values, err
		}
		// TODO: Figure out how to express this in the path registry. For now, just return the resource names.
		// This assumes the live state contains only the most recent resources.
		for _, doc := range parsedLiveState {
			resourceCategory, err := k8skit.K8sResourceProvider.ResourceCategoryGetter(doc)
			if err != nil {
				return parsedData, nil, err
			}
			resourceType, err := k8skit.K8sResourceProvider.ResourceTypeGetter(doc)
			if err != nil {
				return parsedData, nil, err
			}
			resourceName, err := k8skit.K8sResourceProvider.ResourceNameGetter(doc)
			if err != nil {
				return parsedData, nil, err
			}
			scopelessResourceName := k8skit.K8sResourceProvider.RemoveScopeFromResourceName(resourceName)
			// The getter is needed for matching in the resolve process.
			getterFunctionInvocation := &api.FunctionInvocation{
				FunctionName: "get-resources-of-type",
				Arguments:    []api.FunctionArgument{{ParameterName: "resource-type", Value: "v1/ConfigMap"}},
			}
			attributeValue := api.AttributeValue{
				AttributeInfo: api.AttributeInfo{
					AttributeIdentifier: api.AttributeIdentifier{
						ResourceInfo: api.ResourceInfo{
							ResourceName:             resourceName,
							ResourceNameWithoutScope: scopelessResourceName,
							ResourceType:             resourceType,
							ResourceCategory:         resourceCategory,
						},
						Path:        "metadata.name",
						InLiveState: true,
					},
					AttributeMetadata: api.AttributeMetadata{
						AttributeName: api.AttributeNameResourceName,
						DataType:      api.DataTypeString,
						Info: &api.AttributeDetails{
							GetterInvocation: getterFunctionInvocation,
						},
					},
				},
				Value: scopelessResourceName,
			}
			values = append(values, attributeValue)
		}
	}
	return parsedData, values, nil
}

func genericFnCELValidate(resourceProvider yamlkit.ResourceProvider, functionContext *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, _ []byte) (gaby.Container, any, error) {
	validationExpr := args[0].Value.(string)

	env, err := cel.NewEnv(
		cel.Variable("r", cel.DynType),
	)
	if err != nil {
		return parsedData, api.ValidationResultFalse, fmt.Errorf("failed to create CEL environment: %v", err)
	}

	expr, issues := env.Compile(validationExpr)
	if issues != nil {
		return parsedData, api.ValidationResultFalse, fmt.Errorf("failed to compile expression %s: %v", validationExpr, issues)
	}

	if !expr.OutputType().IsExactType(cel.BoolType) {
		return parsedData, api.ValidationResultFalse, fmt.Errorf("expression %s does not evaluate to a boolean", validationExpr)
	}

	program, err := env.Program(expr)
	if err != nil {
		return parsedData, api.ValidationResultFalse, fmt.Errorf("failed to create program for expression %s: %v", validationExpr, err)
	}

	multiErrors := []error{}
	details := []string{}
	passed := true
	for _, doc := range parsedData {
		var dataMap map[string]any
		if err := yaml.Unmarshal(doc.Bytes(), &dataMap); err != nil {
			return parsedData, api.ValidationResultFalse, fmt.Errorf("failed to unmarshal data for config %s: %v", functionContext.UnitDisplayName, err)
		}

		obj := map[string]any{
			"r": dataMap,
		}

		resourceName, err := resourceProvider.ResourceNameGetter(doc)
		if err != nil {
			multiErrors = append(multiErrors, errors.Wrap(err, "could not extract resource name"))
			resourceName = "unknown"
		}
		val, _, err := program.Eval(obj)
		if err != nil {
			passed = false
			multiErrors = append(multiErrors, errors.Wrap(err, "validation expression "+validationExpr+" resulted in error on resource "+string(resourceName)))
			continue
		}
		if val != types.True {
			passed = false
			details = append(details, "resource "+string(resourceName)+" failed validation expression "+validationExpr)
		}
	}

	if passed {
		return parsedData, api.ValidationResultTrue, nil
	}

	failedResult := api.ValidationResultFalse
	failedResult.Details = details
	return parsedData, failedResult, errors.Join(multiErrors...)
}

// Path expressions support embedded accessors and escaped dots.
// They also support wildcards and associative matches.
// Kubernetes annotations and labels permit slashes
var parameterNameRegexpString = "(?:[A-Za-z][A-Za-z0-9_\\-]{0,127})"
var pathMapSegmentRegexpString = "(?:[A-Za-z](?:[A-Za-z0-9/_\\-]|(?:\\~[12])){0,127})"
var pathMapSegmentBoundtoParameterRegexpString = "(?:@" + pathMapSegmentRegexpString + "\\:" + parameterNameRegexpString + ")"
var pathIndexSegmentRegexpString = "(?:[0-9][0-9]{0,9})"
var pathWildcardSegmentRegexpString = "\\*(?:(?:\\?" + pathMapSegmentRegexpString + "(?:\\:" + parameterNameRegexpString + ")?)|(?:@\\:" + parameterNameRegexpString + "))?"
var pathAssociativeMatchRegexpString = "\\?" + pathMapSegmentRegexpString + "(?:\\:" + parameterNameRegexpString + ")?=[^.][^.]*"
var pathSegmentRegexpString = "(?:" + pathMapSegmentRegexpString + "|" + pathMapSegmentBoundtoParameterRegexpString + "|" + pathIndexSegmentRegexpString + "|" + pathWildcardSegmentRegexpString + "|" + pathAssociativeMatchRegexpString + ")"

// Path segment without patterns (for right side of split)
var pathSegmentWithoutPatternsRegexpString = "(?:" + pathMapSegmentRegexpString + "|" + pathMapSegmentBoundtoParameterRegexpString + "|" + pathIndexSegmentRegexpString + ")"
var pathRegexpString = "^" + pathSegmentRegexpString + "(?:\\." + pathSegmentRegexpString + ")*(?:\\|" + pathSegmentWithoutPatternsRegexpString + "(?:\\." + pathSegmentWithoutPatternsRegexpString + ")*)?(?:#" + pathMapSegmentRegexpString + ")?"
var pathNameRegexp = regexp.MustCompile(pathRegexpString)
var whitespaceRegexpString = "^[ \t][ \t]*"
var whitespaceRegexp = regexp.MustCompile(whitespaceRegexpString)
var relationalOperatorRegexpString = "^(<=|>=|<|>|=|\\!=)"
var relationalOperatorRegexp = regexp.MustCompile(relationalOperatorRegexpString)
var logicalOperatorRegexpString = "^AND"
var logicalOperatorRegexp = regexp.MustCompile(logicalOperatorRegexpString)
var booleanLiteralRegexpString = "^(true|false)"
var booleanLiteralRegexp = regexp.MustCompile(booleanLiteralRegexpString)
var integerLiteralRegexpString = "^[0-9][0-9]{0,9}"
var integerLiteralRegexp = regexp.MustCompile(integerLiteralRegexpString)
var stringLiteralRegexpString = `^'[^'"\\]{0,255}'`
var stringLiteralRegexp = regexp.MustCompile(stringLiteralRegexpString)

const andOperator = "AND"

func parseLiteral(decodedQueryString string) (string, string, api.DataType, error) {
	pos := integerLiteralRegexp.FindStringIndex(decodedQueryString)
	if pos != nil {
		literal := decodedQueryString[pos[0]:pos[1]]
		decodedQueryString = decodedQueryString[pos[1]:]
		return decodedQueryString, literal, api.DataTypeInt, nil
	}
	pos = booleanLiteralRegexp.FindStringIndex(decodedQueryString)
	if pos != nil {
		literal := decodedQueryString[pos[0]:pos[1]]
		decodedQueryString = decodedQueryString[pos[1]:]
		return decodedQueryString, literal, api.DataTypeBool, nil
	}
	pos = stringLiteralRegexp.FindStringIndex(decodedQueryString)
	if pos != nil {
		literal := decodedQueryString[pos[0]:pos[1]]
		decodedQueryString = decodedQueryString[pos[1]:]
		return decodedQueryString, literal, api.DataTypeString, nil
	}

	return decodedQueryString, "", api.DataTypeNone, fmt.Errorf("no operand found at `%s`", decodedQueryString)
}

type relationalExpression struct {
	Path     string
	Operator string
	Literal  string
	DataType api.DataType
	// New fields for split path feature
	VisitorPath string // Left side of | for visitor
	SubPath     string // Right side of | for property check
	IsSplitPath bool   // Whether this uses the | syntax
}

func parseAndValidateBinaryExpression(decodedQueryString string) (string, *relationalExpression, error) {
	var expression relationalExpression

	// Whitespace should have been skipped already
	// For now, first operand is always a path name
	pos := pathNameRegexp.FindStringIndex(decodedQueryString)
	if pos == nil {
		return decodedQueryString, &expression, fmt.Errorf("invalid path at `%s`", decodedQueryString)
	}
	path := decodedQueryString[pos[0]:pos[1]]
	decodedQueryString = skipWhitespace(decodedQueryString[pos[1]:])

	// Check for split path syntax using | separator
	if strings.Contains(path, "|") {
		parts := strings.SplitN(path, "|", 2)
		if len(parts) != 2 {
			return decodedQueryString, &expression, fmt.Errorf("invalid split path syntax at `%s`", path)
		}
		expression.VisitorPath = parts[0]
		expression.SubPath = parts[1]
		expression.IsSplitPath = true
		expression.Path = path // Keep original path for compatibility
	} else {
		expression.Path = path
		expression.IsSplitPath = false
	}

	// Get the operator
	pos = relationalOperatorRegexp.FindStringIndex(decodedQueryString)
	if pos == nil {
		return decodedQueryString, &expression, fmt.Errorf("invalid operator at `%s`", decodedQueryString)
	}
	// Operator should be a valid SQL operator
	operator := decodedQueryString[pos[0]:pos[1]]
	decodedQueryString = skipWhitespace(decodedQueryString[pos[1]:])

	// Second operand must be a literal
	var literal string
	var dataType api.DataType
	var err error
	decodedQueryString, literal, dataType, err = parseLiteral(decodedQueryString)
	if err != nil {
		return decodedQueryString, &expression, err
	}
	if dataType == api.DataTypeBool && (operator != "=" && operator != "!=") {
		return decodedQueryString, &expression, fmt.Errorf("invalid boolean operator `%s`", operator)
	}

	expression.Path = path
	expression.Operator = operator
	expression.Literal = literal
	expression.DataType = dataType
	return decodedQueryString, &expression, nil
}

func skipWhitespace(decodedQueryString string) string {
	pos := whitespaceRegexp.FindStringIndex(decodedQueryString)
	if pos != nil {
		return decodedQueryString[pos[1]:]
	}
	return decodedQueryString
}

func getLogicalOperator(decodedQueryString string) (string, string) {
	pos := logicalOperatorRegexp.FindStringIndex(decodedQueryString)
	if pos != nil {
		return decodedQueryString[pos[1]:], decodedQueryString[pos[0]:pos[1]]
	}
	return decodedQueryString, ""
}

func parseAndValidateWhereFilter(queryString string) ([]*relationalExpression, error) {
	expressions := []*relationalExpression{}

	decodedQueryString := skipWhitespace(queryString)
	for decodedQueryString != "" {
		var expression *relationalExpression
		var err error
		decodedQueryString, expression, err = parseAndValidateBinaryExpression(decodedQueryString)
		if err != nil {
			return expressions, err
		}
		expressions = append(expressions, expression)
		decodedQueryString = skipWhitespace(decodedQueryString)
		var operator string
		decodedQueryString, operator = getLogicalOperator(decodedQueryString)
		if operator == andOperator {
			decodedQueryString = skipWhitespace(decodedQueryString)
		}
	}

	return expressions, nil
}

func evaluateStringRelationalExpression(expr *relationalExpression, pathValue string) bool {
	stringLiteral := strings.Trim(expr.Literal, "'")
	switch expr.Operator {
	case "=":
		return pathValue == stringLiteral
	case "!=":
		return pathValue != stringLiteral
	case "<":
		return pathValue < stringLiteral
	case "<=":
		return pathValue <= stringLiteral
	case ">":
		return pathValue > stringLiteral
	case ">=":
		return pathValue >= stringLiteral
	}
	return false
}

func evaluateIntRelationalExpression(expr *relationalExpression, pathValue int) bool {
	intLiteral, err := strconv.Atoi(expr.Literal)
	if err != nil {
		return false
	}
	switch expr.Operator {
	case "=":
		return pathValue == intLiteral
	case "!=":
		return pathValue != intLiteral
	case "<":
		return pathValue < intLiteral
	case "<=":
		return pathValue <= intLiteral
	case ">":
		return pathValue > intLiteral
	case ">=":
		return pathValue >= intLiteral
	}
	return false
}

func evaluateBoolRelationalExpression(expr *relationalExpression, pathValue bool) bool {
	boolLiteral := expr.Literal == "true"
	switch expr.Operator {
	case "=":
		return pathValue == boolLiteral
	case "!=":
		return pathValue != boolLiteral
	}
	return false
}

// evaluateSplitPathExpression handles the split path syntax with | separator
func evaluateSplitPathExpression(expression *relationalExpression, resourceType string, resourceProvider yamlkit.ResourceProvider, parsedData gaby.Container) (map[string]bool, error) {
	matchingResources := map[string]bool{}

	// Use VisitPathsDoc to get to the subobjects using the visitor path (left side of |)
	resourceTypeToPaths := GetVisitorMapForPath(resourceProvider, api.ResourceType(resourceType), api.UnresolvedPath(expression.VisitorPath))

	// Custom visitor function that checks the subpath
	visitor := func(doc *gaby.YamlDoc, output any, context yamlkit.VisitorContext, currentDoc *gaby.YamlDoc) (any, error) {
		// Try to get the value at the subpath within this subobject
		value, found, err := yamlkit.YamlSafePathGetValueAnyType(currentDoc, api.ResolvedPath(expression.SubPath), true)

		var matches bool
		if err != nil {
			return output, err
		}

		if !found {
			// Property not present - handle special case for != operator
			if expression.Operator == "!=" {
				matches = true // != always evaluates to true for missing properties
			} else {
				matches = false // Other operators evaluate to false for missing properties
			}
		} else {
			// Property is present - evaluate normally
			switch expression.DataType {
			case api.DataTypeString:
				if stringValue, ok := value.(string); ok {
					matches = evaluateStringRelationalExpression(expression, stringValue)
				}
			case api.DataTypeInt:
				if intValue, ok := value.(int); ok {
					matches = evaluateIntRelationalExpression(expression, intValue)
				} else if floatValue, ok := value.(float64); ok {
					// Handle JSON numbers that parse as float64
					matches = evaluateIntRelationalExpression(expression, int(floatValue))
				}
			case api.DataTypeBool:
				if boolValue, ok := value.(bool); ok {
					matches = evaluateBoolRelationalExpression(expression, boolValue)
				}
			}
		}

		if matches {
			if existingOutput, ok := output.(map[string]bool); ok {
				existingOutput[string(context.ResourceName)] = true
			}
		}

		return output, nil
	}

	_, err := yamlkit.VisitPathsDoc(parsedData, resourceTypeToPaths, []any{}, matchingResources, resourceProvider, visitor)
	if err != nil {
		return nil, err
	}

	return matchingResources, nil
}

func genericFnResourceWhereMatch(resourceProvider yamlkit.ResourceProvider, functionContext *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, liveState []byte) (gaby.Container, any, error) {
	resourceType := args[0].Value.(string)
	whereExpr := args[1].Value.(string)

	// Allow blank whereExpr: filter by resourceType only
	if strings.TrimSpace(whereExpr) == "" {
		_, categoryTypeMap, err := yamlkit.ResourceAndCategoryTypeMaps(parsedData, resourceProvider)
		if err != nil {
			return parsedData, api.ValidationResultFalse, err
		}
		for categoryType, names := range categoryTypeMap {
			// Ignore the category for now.
			if categoryType.ResourceType == api.ResourceType(resourceType) && len(names) > 0 {
				return parsedData, api.ValidationResultTrue, nil
			}
		}
		return parsedData, api.ValidationResultFalse, nil
	}

	expressions, err := parseAndValidateWhereFilter(whereExpr)
	if err != nil {
		return parsedData, api.ValidationResultFalse, err
	}
	// Visit and evaluate.
	// If we allow wildcards, then theoretically the evaluation could be combinatoric to compare
	// every combination of matching paths. Luckily because we support only conjunctions, which
	// are commutative, we don't need to compare every combination. We can compare them independently
	// in any order. If any expression evaluates to false for a path that exists, then the resource
	// is not a match. However, if any resource does match, then the config Unit should match.
	// We could provide another function that accepts multiple expressions and applies a top-level
	// disjunction to them to allow for selection (e.g., based on resource type) and validation.
	// With exactly 2 expressions we could pass validation if !match_expr || validate_expr.
	var multiErrs []error
	var output any
	matchingResources := map[string]bool{}
	for i, expression := range expressions {
		// The visitor functions visit all resources of the specified type.
		// We need to keep track of which resources have matched.
		// If no paths are found for a resource, that's not a match.
		// If there are errors finding any paths, that's not a match.

		if expression.IsSplitPath {
			// Handle split path syntax with | separator
			matchingResourcesForExpression, err := evaluateSplitPathExpression(expression, resourceType, resourceProvider, parsedData)
			if err != nil {
				multiErrs = append(multiErrs, err)
				matchingResources = nil
				break
			}
			if i == 0 {
				matchingResources = matchingResourcesForExpression
			} else {
				for resourceName, _ := range matchingResources {
					_, matched := matchingResourcesForExpression[resourceName]
					if !matched {
						delete(matchingResources, resourceName)
					}
				}
			}
		} else {
			// Handle original path syntax
			getterArgs := make([]api.FunctionArgument, 2)
			getterArgs[0].Value = resourceType
			getterArgs[1].Value = expression.Path
			switch expression.DataType {
			case api.DataTypeString:
				_, output, err = GenericFnGetStringPath(resourceProvider, functionContext, parsedData, getterArgs, liveState)
			case api.DataTypeInt:
				_, output, err = GenericFnGetIntPath(resourceProvider, functionContext, parsedData, getterArgs, liveState)
			case api.DataTypeBool:
				_, output, err = GenericFnGetBoolPath(resourceProvider, functionContext, parsedData, getterArgs, liveState)
			default:
				err = fmt.Errorf("unsupported data type %s", expression.DataType)
			}
			if err != nil {
				multiErrs = append(multiErrs, err)
				matchingResources = nil
				break
			}

			matchingResourcesForExpression := map[string]bool{}
			attribValues, ok := output.(api.AttributeValueList)
			if !ok {
				log.Errorf("couldn't convert output to api.AttributeValueList")
				multiErrs = append(multiErrs, fmt.Errorf("internal error"))
				continue
			}
			for _, attribValue := range attribValues {
				//fmt.Printf("path: %s\n", attribValue.Path)
				var found bool
				switch expression.DataType {
				case api.DataTypeString:
					stringValue, ok := attribValue.Value.(string)
					if !ok {
						multiErrs = append(multiErrs, fmt.Errorf("internal error"))
					} else {
						found = evaluateStringRelationalExpression(expression, stringValue)
					}
				case api.DataTypeInt:
					intValue, ok := attribValue.Value.(int)
					if !ok {
						multiErrs = append(multiErrs, fmt.Errorf("internal error"))
					} else {
						found = evaluateIntRelationalExpression(expression, intValue)
					}
				case api.DataTypeBool:
					boolValue, ok := attribValue.Value.(bool)
					if !ok {
						multiErrs = append(multiErrs, fmt.Errorf("internal error"))
					} else {
						found = evaluateBoolRelationalExpression(expression, boolValue)
					}
				}
				if found {
					matchingResourcesForExpression[string(attribValue.ResourceName)] = true
				}
			}
			if i == 0 {
				matchingResources = matchingResourcesForExpression
			} else {
				for resourceName, _ := range matchingResources {
					_, matched := matchingResourcesForExpression[resourceName]
					if !matched {
						delete(matchingResources, resourceName)
					}
				}
			}
		}
	}
	if len(multiErrs) != 0 {
		err = errors.Join(multiErrs...)
		return parsedData, api.ValidationResultFalse, err
	}
	if len(matchingResources) > 0 {
		return parsedData, api.ValidationResultTrue, nil
	}
	return parsedData, api.ValidationResultFalse, nil
}

func genericFnComputeMutations(converter configkit.ConfigConverter, resourceProvider yamlkit.ResourceProvider, _ *api.FunctionContext, modifiedParsedData gaby.Container, args []api.FunctionArgument, _ []byte) (gaby.Container, any, error) {
	configStringData := args[0].Value.(string)
	functionIndex := int64(args[1].Value.(int))
	alreadyConverted := false
	if len(args) > 2 {
		alreadyConverted = args[2].Value.(bool)
	}

	var err error
	yamlData := []byte(configStringData)
	if !alreadyConverted {
		yamlData, err = converter.NativeToYAML(yamlData)
		if err != nil {
			return modifiedParsedData, nil, err
		}
	}
	previousParsedData, err := gaby.ParseAll(yamlData)
	if err != nil {
		return modifiedParsedData, nil, err
	}

	mutations, err := yamlkit.ComputeMutations(previousParsedData, modifiedParsedData, functionIndex, resourceProvider)
	return modifiedParsedData, mutations, err
}

func genericFnPatchMutations(resourceProvider yamlkit.ResourceProvider, _ *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, _ []byte) (gaby.Container, any, error) {
	mutationPredicatesString := args[0].Value.(string)
	var mutationsPredicates api.ResourceMutationList
	err := json.Unmarshal([]byte(mutationPredicatesString), &mutationsPredicates)
	if err != nil {
		return parsedData, nil, err
	}
	mutationPatchString := args[1].Value.(string)
	var mutationsPatch api.ResourceMutationList
	err = json.Unmarshal([]byte(mutationPatchString), &mutationsPatch)
	if err != nil {
		return parsedData, nil, err
	}

	parsedData, err = yamlkit.PatchMutations(parsedData, mutationsPredicates, mutationsPatch, resourceProvider)
	return parsedData, nil, err
}

func genericFnReset(resourceProvider yamlkit.ResourceProvider, _ *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, _ []byte) (gaby.Container, any, error) {
	mutationPredicatesString := args[0].Value.(string)
	var mutationsPredicates api.ResourceMutationList
	err := json.Unmarshal([]byte(mutationPredicatesString), &mutationsPredicates)
	if err != nil {
		return parsedData, nil, err
	}

	err = yamlkit.Reset(parsedData, mutationsPredicates, resourceProvider)
	return parsedData, nil, err
}

func genericFnYQ(resourceProvider yamlkit.ResourceProvider, _ *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, _ []byte) (gaby.Container, any, error) {
	// The argument value types should be verified before this function is called
	expression := args[0].Value.(string)

	output, err := yamlkit.EvalYQExpression(expression, parsedData.String())
	wrappedOutput := api.YAMLPayload{Payload: output}
	return parsedData, wrappedOutput, err
}

func genericFnIsApproved(resourceProvider yamlkit.ResourceProvider, functionContext *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, _ []byte) (gaby.Container, any, error) {
	numApprovers := args[0].Value.(int)

	// If the data has changed, previous approvers will be cleared.
	newHash := api.HashConfigData([]byte(parsedData.String()))
	if newHash != functionContext.PreviousContentHash {
		return parsedData, api.ValidationResultFalse, nil
	}

	if len(functionContext.ApprovedBy) >= numApprovers {
		return parsedData, api.ValidationResultTrue, nil
	}
	return parsedData, api.ValidationResultFalse, nil
}

func genericFnEnsureContext(resourceProvider yamlkit.ResourceProvider, functionContext *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, _ []byte) (gaby.Container, any, error) {
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

// genericFnGetDetails.
func genericFnGetDetails(resourceProvider yamlkit.ResourceProvider, _ *api.FunctionContext, parsedData gaby.Container, _ []api.FunctionArgument, _ []byte) (gaby.Container, any, error) {
	detailPaths := yamlkit.GetPathRegistryForAttributeName(resourceProvider, api.AttributeNameDetail)
	values, err := yamlkit.GetPathsAnyType(parsedData, detailPaths, []any{}, resourceProvider, api.DataTypeNone, false)
	return parsedData, values, err
}

func genericFnReplicate(resourceProvider yamlkit.ResourceProvider, functionContext *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, _ []byte) (gaby.Container, any, error) {
	matchResourceType := api.ResourceType(args[0].Value.(string))
	matchResourceName := api.ResourceName(args[1].Value.(string))
	replicas := args[2].Value.(int)
	var matchResourceCategory api.ResourceCategory
	if len(args) > 3 {
		matchResourceCategory = api.ResourceCategory(args[3].Value.(string))
	} else {
		// Default category.
		matchResourceCategory = resourceProvider.DefaultResourceCategory()
	}

	for i, doc := range parsedData {
		resourceCategory, err := resourceProvider.ResourceCategoryGetter(doc)
		if err != nil {
			return parsedData, nil, err
		}
		resourceType, err := resourceProvider.ResourceTypeGetter(doc)
		if err != nil {
			return parsedData, nil, err
		}
		resourceName, err := resourceProvider.ResourceNameGetter(doc)
		if err != nil {
			return parsedData, nil, err
		}
		resourceName = resourceProvider.RemoveScopeFromResourceName(resourceName)
		// fmt.Printf("%s %s %s\n", string(resourceCategory), string(resourceType), string(resourceName))
		if resourceCategory != matchResourceCategory ||
			resourceType != matchResourceType ||
			resourceName != matchResourceName {
			continue
		}
		// Replicate this resource by insertion
		newParsedData := make(gaby.Container, len(parsedData)+replicas-1)
		for j := 0; j < i; j++ {
			newParsedData[j] = parsedData[j]
		}
		for j := 0; j < replicas; j++ {
			replicatedResource := parsedData[i].Bytes()
			parsedReplicatedResource, err := gaby.ParseYAML(replicatedResource)
			if err != nil {
				return parsedData, nil, err
			}
			// TODO: This uniquifies the resource name, but not other attributes in the resource, if required.
			err = resourceProvider.SetResourceName(parsedReplicatedResource, fmt.Sprintf("%s%d", string(resourceName), j))
			newParsedData[i+j] = parsedReplicatedResource
		}
		for j := i + 1; j < len(parsedData); j++ {
			newParsedData[j+replicas-1] = parsedData[j]
		}
		return newParsedData, nil, nil
	}
	return parsedData, nil, nil
}

func genericFnUpsertResource(resourceProvider yamlkit.ResourceProvider, functionContext *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, liveState []byte) (gaby.Container, any, error) {
	// Unmarshal the first argument into api.ResourceList
	resourceListString := args[0].Value.(string)
	var resourceList api.ResourceList
	err := json.Unmarshal([]byte(resourceListString), &resourceList)
	if err != nil {
		return parsedData, nil, fmt.Errorf("failed to unmarshal resource-list argument: %v", err)
	}

	if len(resourceList) == 0 {
		return parsedData, nil, fmt.Errorf("resource-list cannot be empty")
	}

	targetResourceType := api.ResourceType(args[1].Value.(string))
	targetResourceName := api.ResourceName(args[2].Value.(string))

	// Find the resource to upsert from the resource list
	var resourceToUpsert *api.Resource
	for i := range resourceList {
		if resourceList[i].ResourceType == targetResourceType &&
			resourceProvider.RemoveScopeFromResourceName(resourceList[i].ResourceName) == resourceProvider.RemoveScopeFromResourceName(targetResourceName) {
			resourceToUpsert = &resourceList[i]
			break
		}
	}

	if resourceToUpsert == nil {
		return parsedData, nil, fmt.Errorf("resource with type %s and name %s not found in resource-list", targetResourceType, targetResourceName)
	}

	// Parse the resource body to get a document we can insert/replace
	resourceDoc, err := gaby.ParseYAML([]byte(resourceToUpsert.ResourceBody))
	if err != nil {
		return parsedData, nil, fmt.Errorf("failed to parse resource body: %v", err)
	}

	// Use VisitResources to find the existing resource and track its position
	foundIndex := -1
	visitor := func(doc *gaby.YamlDoc, output any, index int, resourceInfo *api.ResourceInfo) (any, []error) {
		if resourceInfo.ResourceType == targetResourceType &&
			resourceProvider.RemoveScopeFromResourceName(resourceInfo.ResourceName) == resourceProvider.RemoveScopeFromResourceName(targetResourceName) {
			foundIndex = index
		}
		return output, []error{}
	}

	_, err = yamlkit.VisitResources(parsedData, nil, resourceProvider, visitor)
	if err != nil {
		return parsedData, nil, fmt.Errorf("failed to search for existing resource: %v", err)
	}

	if foundIndex >= 0 {
		// Replace existing resource
		parsedData[foundIndex] = resourceDoc
	} else {
		// Append new resource
		parsedData = append(parsedData, resourceDoc)
	}

	return parsedData, nil, nil
}

func genericFnDeleteResource(resourceProvider yamlkit.ResourceProvider, functionContext *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, liveState []byte) (gaby.Container, any, error) {
	targetResourceType := api.ResourceType(args[0].Value.(string))
	targetResourceName := api.ResourceName(args[1].Value.(string))

	// Use VisitResources to find the existing resource and track its position
	foundIndex := -1
	visitor := func(doc *gaby.YamlDoc, output any, index int, resourceInfo *api.ResourceInfo) (any, []error) {
		if resourceInfo.ResourceType == targetResourceType &&
			resourceProvider.RemoveScopeFromResourceName(resourceInfo.ResourceName) == resourceProvider.RemoveScopeFromResourceName(targetResourceName) {
			foundIndex = index
		}
		return output, []error{}
	}

	_, err := yamlkit.VisitResources(parsedData, nil, resourceProvider, visitor)
	if err != nil {
		return parsedData, nil, fmt.Errorf("failed to search for resource to delete: %v", err)
	}

	if foundIndex < 0 {
		return parsedData, nil, fmt.Errorf("resource with type %s and name %s not found", targetResourceType, targetResourceName)
	}

	// Remove the resource by creating a new slice without it
	newParsedData := make(gaby.Container, len(parsedData)-1)
	for i := 0; i < foundIndex; i++ {
		newParsedData[i] = parsedData[i]
	}
	for i := foundIndex + 1; i < len(parsedData); i++ {
		newParsedData[i-1] = parsedData[i]
	}

	return newParsedData, nil, nil
}

// Generalized path setter and getter functions moved from kubernetes/container_functions.go

func RegisterPathSetterAndGetter(
	fh handler.FunctionRegistry,
	name string,
	parameters []api.FunctionParameter,
	description string,
	attributeName api.AttributeName,
	resourceProvider yamlkit.ResourceProvider,
	addSetter bool,
) {
	resourceTypes := yamlkit.ResourceTypesForAttribute(attributeName, resourceProvider)
	numSetterParameters := len(parameters)
	// Note that there should be at least one parameter to describe the output.
	numGetterParameters := len(parameters) - 1
	valueParameter := numGetterParameters
	setterParameters := make([]api.FunctionParameter, numSetterParameters)
	for i := range setterParameters {
		setterParameters[i] = parameters[i]
		// All but the last parameter are path parameters
		if i < valueParameter {
			setterParameters[i].Description += "set"
		}
	}
	setterSignature := &api.FunctionSignature{
		FunctionName:          "set-" + name,
		Parameters:            setterParameters,
		Mutating:              true,
		Validating:            false,
		Hermetic:              true,
		Idempotent:            true,
		Description:           "Set" + description,
		FunctionType:          api.FunctionTypePathVisitor,
		AttributeName:         attributeName,
		AffectedResourceTypes: resourceTypes,
	}
	getterParameters := make([]api.FunctionParameter, numGetterParameters)
	for i := range getterParameters {
		getterParameters[i] = parameters[i]
		// All parameters are path parameters
		getterParameters[i].Description += "get"
	}
	// The getter output should match the last setter parameter
	outputInfo := &api.FunctionOutput{
		ResultName:  setterParameters[valueParameter].ParameterName,
		Description: setterParameters[valueParameter].Description,
		OutputType:  api.OutputTypeAttributeValueList,
	}
	getterSignature := &api.FunctionSignature{
		FunctionName:          "get-" + name,
		Parameters:            getterParameters,
		OutputInfo:            outputInfo,
		Mutating:              false,
		Validating:            false,
		Hermetic:              true,
		Idempotent:            true,
		Description:           "Get" + description,
		FunctionType:          api.FunctionTypePathVisitor,
		AttributeName:         attributeName,
		AffectedResourceTypes: resourceTypes,
	}
	var setterFunction, getterFunction handler.FunctionImplementation
	dataType := setterParameters[len(setterParameters)-1].DataType
	switch dataType {
	case api.DataTypeString:
		setterFunction = func(fc *api.FunctionContext, c gaby.Container, fa []api.FunctionArgument, ls []byte) (gaby.Container, any, error) {
			return genericFnSetStringVisitor(setterSignature, fc, c, fa, ls, resourceProvider)
		}
		getterFunction = func(fc *api.FunctionContext, c gaby.Container, fa []api.FunctionArgument, ls []byte) (gaby.Container, any, error) {
			return genericFnGetStringVisitor(getterSignature, fc, c, fa, ls, resourceProvider)
		}
	case api.DataTypeInt:
		setterFunction = func(fc *api.FunctionContext, c gaby.Container, fa []api.FunctionArgument, ls []byte) (gaby.Container, any, error) {
			return genericFnSetIntVisitor(setterSignature, fc, c, fa, ls, resourceProvider)
		}
		getterFunction = func(fc *api.FunctionContext, c gaby.Container, fa []api.FunctionArgument, ls []byte) (gaby.Container, any, error) {
			return genericFnGetIntVisitor(getterSignature, fc, c, fa, ls, resourceProvider)
		}
	case api.DataTypeBool:
		setterFunction = func(fc *api.FunctionContext, c gaby.Container, fa []api.FunctionArgument, ls []byte) (gaby.Container, any, error) {
			return genericFnSetBoolVisitor(setterSignature, fc, c, fa, ls, resourceProvider)
		}
		getterFunction = func(fc *api.FunctionContext, c gaby.Container, fa []api.FunctionArgument, ls []byte) (gaby.Container, any, error) {
			return genericFnGetBoolVisitor(getterSignature, fc, c, fa, ls, resourceProvider)
		}
	default:
		// Not supported
		log.Error("unsupported setter/getter data type " + string(dataType))
		return
	}
	if addSetter {
		fh.RegisterFunction("set-"+name, &handler.FunctionRegistration{
			FunctionSignature: *setterSignature,
			Function:          setterFunction,
		})
	}
	fh.RegisterFunction("get-"+name, &handler.FunctionRegistration{
		FunctionSignature: *getterSignature,
		Function:          getterFunction,
	})
}

func genericFnSetStringVisitor(signature *api.FunctionSignature, _ *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, _ []byte, resourceProvider yamlkit.ResourceProvider) (gaby.Container, any, error) {
	// The argument value types should be verified before this function is called

	// All but the last argument should be path arguments. The last argument is the value to set.
	pathArgs := make([]any, len(args)-1)
	for i := range pathArgs {
		pathArg, ok := args[i].Value.(string)
		if !ok {
			return parsedData, nil, errors.New("Invalid primary FunctionArgument")
		}
		safeArg := yamlkit.EscapeDotsInPathSegment(pathArg)
		pathArgs[i] = safeArg
	}
	valueToSet := args[len(args)-1].Value.(string)

	resourceTypeToPaths := yamlkit.GetPathRegistryForAttributeName(resourceProvider, signature.AttributeName)
	err := yamlkit.UpdateStringPaths(parsedData, resourceTypeToPaths, pathArgs, resourceProvider, valueToSet)
	return parsedData, nil, err
}

func genericFnGetStringVisitor(signature *api.FunctionSignature, _ *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, _ []byte, resourceProvider yamlkit.ResourceProvider) (gaby.Container, any, error) {
	// The argument value types should be verified before this function is called

	// All arguments should be path arguments.
	pathArgs := make([]any, len(args))
	for i := range pathArgs {
		pathArg, ok := args[i].Value.(string)
		if !ok {
			return parsedData, nil, errors.New("Invalid primary FunctionArgument")
		}
		safeArg := yamlkit.EscapeDotsInPathSegment(pathArg)
		pathArgs[i] = safeArg
	}

	resourceTypeToPaths := yamlkit.GetPathRegistryForAttributeName(resourceProvider, signature.AttributeName)
	values, err := yamlkit.GetStringPaths(parsedData, resourceTypeToPaths, pathArgs, resourceProvider)
	return parsedData, values, err
}

func genericFnSetIntVisitor(signature *api.FunctionSignature, _ *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, _ []byte, resourceProvider yamlkit.ResourceProvider) (gaby.Container, any, error) {
	// The argument value types should be verified before this function is called

	// All but the last argument should be path arguments. The last argument is the value to set.
	pathArgs := make([]any, len(args)-1)
	for i := range pathArgs {
		pathArg, ok := args[i].Value.(string)
		if !ok {
			return parsedData, nil, errors.New("Invalid primary FunctionArgument")
		}
		safeArg := yamlkit.EscapeDotsInPathSegment(pathArg)
		pathArgs[i] = safeArg
	}
	valueToSet := args[len(args)-1].Value.(int)

	resourceTypeToPaths := yamlkit.GetPathRegistryForAttributeName(resourceProvider, signature.AttributeName)
	err := yamlkit.UpdatePathsValue[int](parsedData, resourceTypeToPaths, pathArgs, resourceProvider, valueToSet)
	return parsedData, nil, err
}

func genericFnGetIntVisitor(signature *api.FunctionSignature, _ *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, _ []byte, resourceProvider yamlkit.ResourceProvider) (gaby.Container, any, error) {
	// The argument value types should be verified before this function is called

	// All arguments should be path arguments.
	pathArgs := make([]any, len(args))
	for i := range pathArgs {
		pathArg, ok := args[i].Value.(string)
		if !ok {
			return parsedData, nil, errors.New("Invalid primary FunctionArgument")
		}
		safeArg := yamlkit.EscapeDotsInPathSegment(pathArg)
		pathArgs[i] = safeArg
	}

	resourceTypeToPaths := yamlkit.GetPathRegistryForAttributeName(resourceProvider, signature.AttributeName)
	values, err := yamlkit.GetPaths[int](parsedData, resourceTypeToPaths, pathArgs, resourceProvider)
	return parsedData, values, err
}

func genericFnSetBoolVisitor(signature *api.FunctionSignature, _ *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, _ []byte, resourceProvider yamlkit.ResourceProvider) (gaby.Container, any, error) {
	// The argument value types should be verified before this function is called

	// All but the last argument should be path arguments. The last argument is the value to set.
	pathArgs := make([]any, len(args)-1)
	for i := range pathArgs {
		pathArg, ok := args[i].Value.(string)
		if !ok {
			return parsedData, nil, errors.New("Invalid primary FunctionArgument")
		}
		safeArg := yamlkit.EscapeDotsInPathSegment(pathArg)
		pathArgs[i] = safeArg
	}
	valueToSet := args[len(args)-1].Value.(bool)

	resourceTypeToPaths := yamlkit.GetPathRegistryForAttributeName(resourceProvider, signature.AttributeName)
	err := yamlkit.UpdatePathsValue[bool](parsedData, resourceTypeToPaths, pathArgs, resourceProvider, valueToSet)
	return parsedData, nil, err
}

func genericFnGetBoolVisitor(signature *api.FunctionSignature, _ *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, _ []byte, resourceProvider yamlkit.ResourceProvider) (gaby.Container, any, error) {
	// The argument value types should be verified before this function is called

	// All arguments should be path arguments.
	pathArgs := make([]any, len(args))
	for i := range pathArgs {
		pathArg, ok := args[i].Value.(string)
		if !ok {
			return parsedData, nil, errors.New("Invalid primary FunctionArgument")
		}
		safeArg := yamlkit.EscapeDotsInPathSegment(pathArg)
		pathArgs[i] = safeArg
	}

	resourceTypeToPaths := yamlkit.GetPathRegistryForAttributeName(resourceProvider, signature.AttributeName)
	values, err := yamlkit.GetPaths[bool](parsedData, resourceTypeToPaths, pathArgs, resourceProvider)
	return parsedData, values, err
}
