// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package generic

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/cockroachdb/errors/join"
	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/labstack/gommon/log"
	"github.com/swaggest/jsonschema-go"
	"github.com/xeipuuv/gojsonschema"
	"sigs.k8s.io/yaml"

	"github.com/confighub/sdk/configkit"
	"github.com/confighub/sdk/configkit/k8skit"
	"github.com/confighub/sdk/configkit/yamlkit"
	"github.com/confighub/sdk/function/api"
	"github.com/confighub/sdk/function/handler"
	"github.com/confighub/sdk/third_party/gaby"
)

func RegisterComputeMutations(fh handler.FunctionRegistry, converter configkit.ConfigConverter, resourceProvider yamlkit.ResourceProvider) {
	reflector := jsonschema.Reflector{}
	resourceMutationListSchema, err := reflector.Reflect(api.ResourceMutationList{})
	if err != nil {
		log.Errorf("couldn't get schema for api.ResourceMutationList")
	}
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
					ParameterName: "function-index",
					Required:      true,
					Description:   "Index of the function from the invocation list that mutated the config data",
					DataType:      api.DataTypeInt,
					Example:       "0",
				},
				{
					ParameterName: "already-converted",
					Required:      false,
					Description:   "If true, the config-doc-list is already converted to YAML",
					DataType:      api.DataTypeBool,
				},
			},
			OutputInfo: &api.FunctionOutput{
				ResultName:  "mutations",
				Description: "List of resource mutations in the same order as the resources in the config data",
				OutputType:  api.OutputTypeResourceMutationList,
				Schema:      &resourceMutationListSchema,
			},
			Mutating:              false,
			Validating:            false,
			Hermetic:              true,
			Idempotent:            true,
			Description:           `Diffs the previous config data from the parameter with the current config data from the unit and returns a list of resource mutations made to the config data. The output can be used with patch-mutations.`,
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(functionContext *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, liveState []byte) (gaby.Container, any, error) {
			return genericFnComputeMutations(converter, resourceProvider, functionContext, parsedData, args, liveState)
		},
	})
}

func RegisterStandardFunctions(fh handler.FunctionRegistry, converter configkit.ConfigConverter, resourceProvider yamlkit.ResourceProvider) {
	reflector := jsonschema.Reflector{}
	resourceListSchema, err := reflector.Reflect(api.ResourceList{})
	if err != nil {
		log.Errorf("couldn't get schema for api.ResourceList")
	}
	resourceInfoListSchema, err := reflector.Reflect(api.ResourceInfoList{})
	if err != nil {
		log.Errorf("couldn't get schema for api.ResourceInfoList")
	}
	attributeValueListSchema, err := reflector.Reflect(api.AttributeValueList{})
	if err != nil {
		log.Errorf("couldn't get schema for api.AttributeValueList")
	}
	validationResultListSchema, err := reflector.Reflect(api.ValidationResultList{})
	if err != nil {
		log.Errorf("couldn't get schema for api.ValidationResultList")
	}
	yamlPayloadSchema, err := reflector.Reflect(api.YAMLPayload{})
	if err != nil {
		log.Errorf("couldn't get schema for api.YAMLPayload")
	}
	resourceMutationListSchema, err := reflector.Reflect(api.ResourceMutationList{})
	if err != nil {
		log.Errorf("couldn't get schema for api.ResourceMutationList")
	}
	fh.RegisterFunction("get-resources", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "get-resources",
			Parameters: []api.FunctionParameter{
				{
					ParameterName:    "body",
					Required:         false,
					Description:      "Format for resource body output: yaml (default), none, json, or native",
					DataType:         api.DataTypeEnum,
					Example:          "yaml",
					ValueConstraints: api.ValueConstraints{EnumValues: []string{"yaml", "none", "json", "native"}},
				},
			},
			OutputInfo: &api.FunctionOutput{
				ResultName:  "resource",
				Description: "Return the names, types, and bodies of the resources",
				OutputType:  api.OutputTypeResourceList,
				Schema:      &resourceListSchema,
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
			return genericFnGetResources(converter, resourceProvider, functionContext, parsedData, args, liveState)
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
				Schema:      &resourceInfoListSchema,
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
				Schema:      &attributeValueListSchema,
			},
			Mutating:              false,
			Validating:            false,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Returns a list of attributes containing the placeholder string 'confighubplaceholder' or number 999999999. See https://docs.confighub.com/background/concepts/placeholders/ for more information about placeholders.",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(functionContext *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, liveState []byte) (gaby.Container, any, error) {
			return genericFnGetPlaceholders(resourceProvider, functionContext, parsedData, args, liveState)
		},
	})
	fh.RegisterFunction("vet-placeholders", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "vet-placeholders",
			OutputInfo: &api.FunctionOutput{
				ResultName:  "passed",
				Description: "True if no placeholders remain, false otherwise",
				OutputType:  api.OutputTypeValidationResult,
				Schema:      &validationResultListSchema,
			},
			Mutating:              false,
			Validating:            true,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Returns true if no attributes contain the placeholder string 'confighubplaceholder' or number 999999999. See https://docs.confighub.com/background/concepts/placeholders/ for more information about placeholders.",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(functionContext *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, liveState []byte) (gaby.Container, any, error) {
			return genericFnVetPlaceholders(resourceProvider, functionContext, parsedData, args, liveState)
		},
	})
	// TODO: Deprecated in favor of vet-placeholders. Remove this.
	fh.RegisterFunction("no-placeholders", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "no-placeholders",
			OutputInfo: &api.FunctionOutput{
				ResultName:  "passed",
				Description: "True if no placeholders remain, false otherwise",
				OutputType:  api.OutputTypeValidationResult,
				Schema:      &validationResultListSchema,
			},
			Mutating:              false,
			Validating:            true,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "[Deprecated; use vet-placeholders instead] Returns true if no attributes contain the placeholder string 'confighubplaceholder' or number 999999999. See https://docs.confighub.com/background/concepts/placeholders/ for more information about placeholders.",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(functionContext *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, liveState []byte) (gaby.Container, any, error) {
			return genericFnVetPlaceholders(resourceProvider, functionContext, parsedData, args, liveState)
		},
	})
	fh.RegisterFunction("search-replace", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "search-replace",
			Parameters: []api.FunctionParameter{
				{
					ParameterName: "search-value",
					Required:      true,
					Description:   "String value to search for",
					DataType:      api.DataTypeString,
				},
				{
					ParameterName: "replace-value",
					Required:      true,
					Description:   "String value to use as the replacement for search-value",
					DataType:      api.DataTypeString,
				},
			},
			Mutating:              true,
			Validating:            false,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Replace all instances of the search-value in all strings of all resource types with replace-value",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(functionContext *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, liveState []byte) (gaby.Container, any, error) {
			return genericFnSearchReplace(resourceProvider, functionContext, parsedData, args, liveState)
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
					ParameterName:    "path",
					Required:         true,
					Description:      "Dot-separated configuration path of the attribute whose value to get. See https://docs.confighub.com/guide/functions/#configuration-path-syntax for more details regarding path syntax.",
					DataType:         api.DataTypeString,
					ValueConstraints: api.ValueConstraints{Regexp: api.PathRegexpString},
				},
			},
			OutputInfo: &api.FunctionOutput{
				ResultName:  "path",
				Description: "Value of the specified attribute path",
				OutputType:  api.OutputTypeAttributeValueList,
				Schema:      &attributeValueListSchema,
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
					ParameterName:    "path",
					Required:         true,
					Description:      "Dot-separated path of the attribute to set. See https://docs.confighub.com/guide/functions/#configuration-path-syntax for more details regarding path syntax.",
					DataType:         api.DataTypeString,
					ValueConstraints: api.ValueConstraints{Regexp: api.PathRegexpString},
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
			Description:           "Set the value of the specified attribute path",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(functionContext *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, liveState []byte) (gaby.Container, any, error) {
			return GenericFnSetStringPath(resourceProvider, functionContext, parsedData, args, liveState, true)
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
					ParameterName:    "path",
					Required:         true,
					Description:      "Dot-separated path of the attribute whose value to get. See https://docs.confighub.com/guide/functions/#configuration-path-syntax for more details regarding path syntax.",
					DataType:         api.DataTypeString,
					ValueConstraints: api.ValueConstraints{Regexp: api.PathRegexpString},
				},
			},
			OutputInfo: &api.FunctionOutput{
				ResultName:  "path",
				Description: "Value of the specified attribute path",
				OutputType:  api.OutputTypeAttributeValueList,
				Schema:      &attributeValueListSchema,
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
					ParameterName:    "path",
					Required:         true,
					Description:      "Dot-separated path of the attribute to set. See https://docs.confighub.com/guide/functions/#configuration-path-syntax for more details regarding path syntax.",
					DataType:         api.DataTypeString,
					ValueConstraints: api.ValueConstraints{Regexp: api.PathRegexpString},
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
			Description:           "Set the value of the specified attribute path",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(functionContext *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, liveState []byte) (gaby.Container, any, error) {
			return GenericFnSetIntPath(resourceProvider, functionContext, parsedData, args, liveState, true)
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
					ParameterName:    "path",
					Required:         true,
					Description:      "Dot-separated path of the attribute whose value to get. See https://docs.confighub.com/guide/functions/#configuration-path-syntax for more details regarding path syntax.",
					DataType:         api.DataTypeString,
					ValueConstraints: api.ValueConstraints{Regexp: api.PathRegexpString},
				},
			},
			OutputInfo: &api.FunctionOutput{
				ResultName:  "path",
				Description: "Value of the specified attribute path",
				OutputType:  api.OutputTypeAttributeValueList,
				Schema:      &attributeValueListSchema,
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
					ParameterName:    "path",
					Required:         true,
					Description:      "Dot-separated path of the attribute to set. See https://docs.confighub.com/guide/functions/#configuration-path-syntax for more details regarding path syntax.",
					DataType:         api.DataTypeString,
					ValueConstraints: api.ValueConstraints{Regexp: api.PathRegexpString},
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
			Description:           "Set the value of the specified attribute path",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(functionContext *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, liveState []byte) (gaby.Container, any, error) {
			return GenericFnSetBoolPath(resourceProvider, functionContext, parsedData, args, liveState, true)
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
					ParameterName:    "path",
					Required:         true,
					Description:      "Dot-separated path of the attribute to comment. See https://docs.confighub.com/guide/functions/#configuration-path-syntax for more details regarding path syntax.",
					DataType:         api.DataTypeString,
					ValueConstraints: api.ValueConstraints{Regexp: api.PathRegexpString},
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
		Function: func(functionContext *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, liveState []byte) (gaby.Container, any, error) {
			return GenericFnDeletePath(resourceProvider, functionContext, parsedData, args, liveState)
		},
	})
	fh.RegisterFunction("set-default-names", &handler.FunctionRegistration{
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
		Function: func(functionContext *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, liveState []byte) (gaby.Container, any, error) {
			return genericFnSetDefaultNames(resourceProvider, functionContext, parsedData, args, liveState)
		},
	})
	fh.RegisterFunction("get-attribute", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "get-attribute",
			Parameters: []api.FunctionParameter{
				{
					ParameterName:    "attribute-name",
					Required:         true,
					Description:      "Name of the attribute get",
					DataType:         api.DataTypeString,
					ValueConstraints: api.ValueConstraints{Regexp: api.AttributeNamePrefixRegexpString + "$"},
				},
			},
			OutputInfo: &api.FunctionOutput{
				ResultName:  "attribute-list",
				Description: "Specified attribute values",
				OutputType:  api.OutputTypeAttributeValueList,
				Schema:      &attributeValueListSchema,
			},
			Mutating:     false,
			Validating:   false,
			Hermetic:     true,
			Idempotent:   true,
			Description:  "Returns values of a specified registered attribute. See https://docs.confighub.com/guide/functions/#getters-and-setters-attributes-and-the-path-registry for more information about registered attributes.",
			FunctionType: api.FunctionTypePathVisitor,
			// No AttributeName, since that's provided as a parameter
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(functionContext *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, liveState []byte) (gaby.Container, any, error) {
			return genericFnGetAttribute(resourceProvider, functionContext, parsedData, args, liveState)
		},
	})
	fh.RegisterFunction("get-attributes", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "get-attributes",
			OutputInfo: &api.FunctionOutput{
				ResultName:  "attribute-list",
				Description: "Selected attribute values of common resource types",
				OutputType:  api.OutputTypeAttributeValueList,
				Schema:      &attributeValueListSchema,
			},
			Mutating:              false,
			Validating:            false,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Returns a list of selected attribute values. Currently it returns a curated set of attributes registered under " + string(api.AttributeNameGeneral) + ", many of which are also registered under more specific attribute names.",
			FunctionType:          api.FunctionTypePathVisitor,
			AttributeName:         api.AttributeNameGeneral,
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
					ParameterName:    "attribute-list",
					Required:         true,
					Description:      "List of attributes to set",
					DataType:         api.DataTypeAttributeValueList,
					ValueConstraints: api.ValueConstraints{Schema: &attributeValueListSchema},
				},
			},
			Mutating:              true,
			Validating:            false,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Set specified attributes to the specified values. This function is intended to be used for read-modify-write operations in combination with any (typically `get-`) functions returning output of the type " + string(api.DataTypeAttributeValueList) + ".",
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
				ResultName:  "attribute-list",
				Description: "Needed attributes",
				OutputType:  api.OutputTypeAttributeValueList,
				Schema:      &attributeValueListSchema,
			},
			Mutating:              false,
			Validating:            false,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Returns a list of needed attributes with setter functions. See https://docs.confighub.com/background/concepts/needsprovides/ for more information.",
			FunctionType:          api.FunctionTypePathVisitor,
			AttributeName:         api.AttributeNameNeededValue,
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
				ResultName:  "attribute-list",
				Description: "Provided attributes",
				OutputType:  api.OutputTypeAttributeValueList,
				Schema:      &attributeValueListSchema,
			},
			Mutating:              false,
			Validating:            false,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Returns a list of Provided attributes. See https://docs.confighub.com/background/concepts/needsprovides/ for more information.",
			FunctionType:          api.FunctionTypePathVisitor,
			AttributeName:         api.AttributeNameProvidedValue,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(functionContext *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, liveState []byte) (gaby.Container, any, error) {
			return genericFnGetProvided(resourceProvider, functionContext, parsedData, args, liveState)
		},
	})
	fh.RegisterFunction("vet-celexpr", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "vet-celexpr",
			Parameters: []api.FunctionParameter{
				{
					ParameterName: "validation-expr",
					Required:      true,
					Description:   "CEL (Common Expression Language) expression to validate each resource. The current resource is referenced with the prefix 'r.' See https://cel.dev/ for language details.",
					DataType:      api.DataTypeCEL,
					// TODO: Override this with ToolchainType-specific examples.
					Example: "r.kind != 'Deployment' || (r.spec.template.spec.securityContext.runAsNonRoot == true && r.spec.template.spec.containers.all(container, !has(container.securityContext.runAsNonRoot) || container.securityContext.runAsNonRoot == true)) || r.spec.template.spec.containers.all(container, has(container.securityContext.runAsNonRoot) && container.securityContext.runAsNonRoot == true)",
				},
			},
			OutputInfo: &api.FunctionOutput{
				ResultName:  "passed",
				Description: "True if validation passed, false otherwise",
				OutputType:  api.OutputTypeValidationResult,
				Schema:      &validationResultListSchema,
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
	// TODO: Deprecated in favor of vet-celexpr. Remove this.
	fh.RegisterFunction("cel-validate", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "cel-validate",
			Parameters: []api.FunctionParameter{
				{
					ParameterName: "validation-expr",
					Required:      true,
					Description:   "CEL (Common Expression Language) expression to validate each resource. The current resource is refenced with the prefix 'r.' See https://cel.dev/ for language details.",
					DataType:      api.DataTypeCEL,
					// TODO: Override this with ToolchainType-specific examples.
					Example: "r.kind != 'Deployment' || r.spec.template.spec.containers.all(container, container.securityContext.runAsNonRoot == true)",
				},
			},
			OutputInfo: &api.FunctionOutput{
				ResultName:  "passed",
				Description: "True if validation passed, false otherwise",
				OutputType:  api.OutputTypeValidationResult,
				Schema:      &validationResultListSchema,
			},
			Mutating:              false,
			Validating:            true,
			Hermetic:              true,
			Idempotent:            true,
			Description:           `[Deprecated; use vet-celexpr instead] Returns true if validation expression evaluates to true for all resources`,
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
					Description:   "The specified string is an expression for the purpose of evaluating whether the configuration data matches the filter. It supports conjunctions using `AND` of relational expressions of the form *path* *operator* *literal*. The path specifications are dot-separated, for both map fields and array indices, as in `spec.template.spec.containers.0.image = 'ghcr.io/headlamp-k8s/headlamp:latest' AND spec.replicas > 1`. Path expressions support `*` for wildcard array or map segments and `?key=value` syntax for associative matches of array elements containing objects with a `key` attribute. Strings support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `LIKE`, `ILIKE`, `~~`, `!~~`, `~`, `!~`, `~*`, `!~*`, `IN`, `NOT IN`. String pattern operators: `LIKE` and `~~` for pattern matching with `%` and `_` wildcards, `ILIKE` for case-insensitive pattern matching, `!~~` for NOT LIKE. String regex operators: `~` for regex matching, `~*` for case-insensitive regex, `!~` and `!~*` for regex not matching (case-sensitive and insensitive). Integers support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `IN`, `NOT IN`. Boolean values support equality and inequality only. The `IN` and `NOT IN` operators accept a comma-separated list of values in parentheses, such as `spec.template.spec.containers.0.image#reference IN (':latest', ':arm64-latest')`. The syntax `.|` requires the preceding path to exist; otherwise the relation `!=` will always return true regardless what it is compared with. String literals are quoted with single quotes, such as `'string'`. Integer and boolean literals are also supported for attributes of those types.",
					DataType:      api.DataTypeString,
				},
			},
			OutputInfo: &api.FunctionOutput{
				ResultName:  "matched",
				Description: "True if filter passed for at least one resource, false otherwise",
				OutputType:  api.OutputTypeValidationResult,
				Schema:      &validationResultListSchema,
			},
			Mutating:              false,
			Validating:            true,
			Hermetic:              true,
			Idempotent:            true,
			Description:           `Returns true if all terms of the conjunction of relational expressions evaluate to true for at least one matching path of a resource of the specified type. Intended to be used for filtering rather than validating, though it returns the same output type.`,
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
					Example:       ".spec.replicas",
				},
			},
			OutputInfo: &api.FunctionOutput{
				ResultName:  "yq output",
				Description: "Output from yq",
				OutputType:  api.OutputTypeYAML,
				Schema:      &yamlPayloadSchema,
			},
			Mutating:              false,
			Validating:            false,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Returns the result of running yq with the specified expression on the configuration data",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(functionContext *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, liveState []byte) (gaby.Container, any, error) {
			return genericFnYQ(resourceProvider, functionContext, parsedData, args, liveState, false)
		},
	})
	fh.RegisterFunction("yq-i", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "yq-i",
			Parameters: []api.FunctionParameter{
				{
					ParameterName: "yq-expression",
					Required:      true,
					Description:   "yq expression",
					DataType:      api.DataTypeString,
					Example:       ".spec.replicas = 7",
				},
			},
			Mutating:              true,
			Validating:            false,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "The configuration data is updated with the result of running yq -i with the specified expression on the configuration data",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(functionContext *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, liveState []byte) (gaby.Container, any, error) {
			return genericFnYQ(resourceProvider, functionContext, parsedData, args, liveState, true)
		},
	})
	// TODO: Deprecated in favor of vet-approvedby. Remove this.
	fh.RegisterFunction("is-approved", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "is-approved",
			Parameters: []api.FunctionParameter{
				{
					ParameterName: "num-approvers",
					Required:      true,
					Description:   "Number of approvers",
					DataType:      api.DataTypeInt,
					Example:       "2",
				},
			},
			OutputInfo: &api.FunctionOutput{
				ResultName:  "passed",
				Description: "True if approvers are present, false otherwise",
				OutputType:  api.OutputTypeValidationResult,
				Schema:      &validationResultListSchema,
			},
			Mutating:              false,
			Validating:            true,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "[Deprecated; use vet-approvedby instead] Returns true if sufficient approvers are present",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(functionContext *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, liveState []byte) (gaby.Container, any, error) {
			return genericFnVetApprovedBy(resourceProvider, functionContext, parsedData, args, liveState)
		},
	})
	fh.RegisterFunction("vet-approvedby", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "vet-approvedby",
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
				Schema:      &validationResultListSchema,
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
			return genericFnVetApprovedBy(resourceProvider, functionContext, parsedData, args, liveState)
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
			Description:           "Set function context values (e.g., unit slug, space ID) in configuration resource/element attributes (if possible) if add-context is true and remove the context if false. These values can be used to find the corresponding unit in ConfigHub, such as with `cub k8s source`.",
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
				ResultName:  "attribute-list",
				Description: "Selected resource attributes",
				OutputType:  api.OutputTypeAttributeValueList,
				Schema:      &attributeValueListSchema,
			},
			Mutating:              false,
			Validating:            false,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Returns a list of curated resource attributes",
			FunctionType:          api.FunctionTypePathVisitor,
			AttributeName:         api.AttributeNameDetail,
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
					ParameterName:    "resource-list",
					Required:         true,
					Description:      "ResourceList containing the resource to upsert",
					DataType:         api.DataTypeResourceList,
					ValueConstraints: api.ValueConstraints{Schema: &resourceListSchema},
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
			Description:           "Append the resource if it is not present or replace the existing resource if it is already present in the configuration data. Intended to be used with get-resources.",
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
					ParameterName:    "mutation-predicates",
					Required:         true,
					Description:      "Mutations with predicates set to true if they are patchable",
					DataType:         api.DataTypeResourceMutationList,
					ValueConstraints: api.ValueConstraints{Schema: &resourceMutationListSchema},
				},
				{
					ParameterName:    "mutation-patch",
					Required:         true,
					Description:      "Mutations to filter and patch",
					DataType:         api.DataTypeResourceMutationList,
					ValueConstraints: api.ValueConstraints{Schema: &resourceMutationListSchema},
				},
			},
			Mutating:              true,
			Validating:            false,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Selectively patch attributes if their mutations indicate they are patchable. Intended to be used with compute-mutations.",
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
					ParameterName:    "mutation-predicates",
					Required:         true,
					Description:      "Mutations with predicates set to true if they should be reset",
					DataType:         api.DataTypeResourceMutationList,
					ValueConstraints: api.ValueConstraints{Schema: &resourceMutationListSchema},
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
			Description:           "Replicate the specified configuration resource/element replicas minus one times",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(functionContext *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, liveState []byte) (gaby.Container, any, error) {
			return genericFnReplicate(resourceProvider, functionContext, parsedData, args, liveState)
		},
	})
	fh.RegisterFunction("vet-jsonschema", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "vet-jsonschema",
			Parameters: []api.FunctionParameter{
				{
					ParameterName: "schema-map",
					Required:      true,
					Description:   "JSON-encoded map from ResourceType to JSONSchema",
					DataType:      api.DataTypeString,
					Example:       "{\"SimpleApp\": {...}}",
				},
			},
			OutputInfo: &api.FunctionOutput{
				ResultName:  "passed",
				Description: "True if all resources pass schema validation, false otherwise",
				OutputType:  api.OutputTypeValidationResult,
				Schema:      &validationResultListSchema,
			},
			Mutating:              false,
			Validating:            true,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Validates each resource against its corresponding JSONSchema from the provided map",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(functionContext *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, liveState []byte) (gaby.Container, any, error) {
			return GenericFnVetJSONSchema(resourceProvider, functionContext, parsedData, args, liveState)
		},
	})
}

func attributeNameForResourceType(resourceType api.ResourceType) api.AttributeName {
	return api.AttributeName(string(api.AttributeNameResourceName) + "/" + string(resourceType))
}

func genericFnGetResources(converter configkit.ConfigConverter, resourceProvider yamlkit.ResourceProvider, _ *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, _ []byte) (gaby.Container, any, error) {
	// Default body format is "yaml"
	bodyFormat := "yaml"
	if len(args) > 0 {
		bodyFormat = strings.ToLower(args[0].Value.(string))
	}

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

		var resourceBody string
		switch bodyFormat {
		case "none":
			resourceBody = ""
		case "json":
			jsonBytes, err := doc.MarshalJSON()
			if err != nil {
				return parsedData, nil, err
			}
			resourceBody = string(jsonBytes)
		case "native":
			yamlBytes := []byte(doc.String())
			nativeBytes, err := converter.YAMLToNative(yamlBytes)
			if err != nil {
				return parsedData, nil, err
			}
			resourceBody = string(nativeBytes)
		case "yaml":
			fallthrough
		default:
			resourceBody = doc.String()
		}

		list = append(list, api.Resource{
			ResourceInfo: api.ResourceInfo{
				ResourceName:             resourceName,
				ResourceNameWithoutScope: resourceProvider.RemoveScopeFromResourceName(resourceName),
				ResourceType:             resourceType,
				ResourceCategory:         resourceCategory,
			},
			ResourceBody: resourceBody,
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
		err = yamlkit.UpdateStringPaths(parsedData, paths, []any{}, resourceProvider, resourceName, false)
	}
	return parsedData, nil, err
}

func genericFnGetPlaceholders(resourceProvider yamlkit.ResourceProvider, _ *api.FunctionContext, parsedData gaby.Container, _ []api.FunctionArgument, _ []byte) (gaby.Container, any, error) {
	paths := yamlkit.FindYAMLPathsByValue(parsedData, resourceProvider, yamlkit.PlaceHolderBlockApplyString)
	paths = append(paths, yamlkit.FindYAMLPathsByValue(parsedData, resourceProvider, yamlkit.PlaceHolderBlockApplyInt)...)
	return parsedData, paths, nil
}

func genericFnVetPlaceholders(resourceProvider yamlkit.ResourceProvider, _ *api.FunctionContext, parsedData gaby.Container, _ []api.FunctionArgument, _ []byte) (gaby.Container, any, error) {
	paths := yamlkit.FindYAMLPathsByValue(parsedData, resourceProvider, yamlkit.PlaceHolderBlockApplyString)
	paths = append(paths, yamlkit.FindYAMLPathsByValue(parsedData, resourceProvider, yamlkit.PlaceHolderBlockApplyInt)...)
	result := api.ValidationResult{
		Passed:           len(paths) == 0,
		FailedAttributes: paths,
	}
	return parsedData, result, nil
}

func genericFnSearchReplace(resourceProvider yamlkit.ResourceProvider, functionContext *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, liveState []byte) (gaby.Container, any, error) {
	searchValue := args[0].Value.(string)
	replaceValue := args[1].Value.(string)

	attributeList := yamlkit.FindYAMLPathsByValue(parsedData, resourceProvider, searchValue)
	for i := range attributeList {
		existingValue := attributeList[i].Value.(string)
		attributeList[i].Value = strings.ReplaceAll(existingValue, searchValue, replaceValue)
	}

	return genericSetAttributesFromList(resourceProvider, functionContext, parsedData, attributeList, liveState)
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
	if yamlkit.PathIsResolved(string(path), true) {
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

func GenericFnSetStringPath(resourceProvider yamlkit.ResourceProvider, _ *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, _ []byte, upsert bool) (gaby.Container, any, error) {
	// The argument value types should be verified before this function is called
	resourceType := args[0].Value.(string)
	unresolvedPath := args[1].Value.(string)
	value := args[2].Value.(string)

	resourceTypeToPaths := GetVisitorMapForPath(resourceProvider, api.ResourceType(resourceType), api.UnresolvedPath(unresolvedPath))
	err := yamlkit.UpdateStringPaths(parsedData, resourceTypeToPaths, []any{}, resourceProvider, value, upsert)
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

func GenericFnSetIntPath(resourceProvider yamlkit.ResourceProvider, _ *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, _ []byte, upsert bool) (gaby.Container, any, error) {
	// The argument value types should be verified before this function is called
	resourceType := args[0].Value.(string)
	unresolvedPath := args[1].Value.(string)
	value := args[2].Value.(int)

	resourceTypeToPaths := GetVisitorMapForPath(resourceProvider, api.ResourceType(resourceType), api.UnresolvedPath(unresolvedPath))
	err := yamlkit.UpdatePathsValue[int](parsedData, resourceTypeToPaths, []any{}, resourceProvider, value, upsert)
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

func GenericFnSetBoolPath(resourceProvider yamlkit.ResourceProvider, _ *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, _ []byte, upsert bool) (gaby.Container, any, error) {
	// The argument value types should be verified before this function is called
	resourceType := args[0].Value.(string)
	unresolvedPath := args[1].Value.(string)
	value := args[2].Value.(bool)

	resourceTypeToPaths := GetVisitorMapForPath(resourceProvider, api.ResourceType(resourceType), api.UnresolvedPath(unresolvedPath))
	err := yamlkit.UpdatePathsValue[bool](parsedData, resourceTypeToPaths, []any{}, resourceProvider, value, upsert)
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
	_, err := yamlkit.VisitPathsDoc(parsedData, resourceTypeToPaths, []any{}, nil, resourceProvider, visitor, false)
	return parsedData, nil, err
}

// TODO: Remove if not still useful
// func trimResourceName(resourceName, typeName, spaceName, unitName, separator string) string {
// 	// The type may be used as a suffix
// 	name := strings.TrimSuffix(strings.TrimSuffix(resourceName, typeName), separator)
// 	// The unit and space may be used as prefixes
// 	name = strings.TrimPrefix(strings.TrimPrefix(name, unitName), separator)
// 	name = strings.TrimPrefix(strings.TrimPrefix(name, spaceName), separator)
// 	return name
// }

func genericFnSetDefaultNames(resourceProvider yamlkit.ResourceProvider, functionContext *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, _ []byte) (gaby.Container, any, error) {
	nameValue := args[0].Value.(string)

	visitor := func(doc *gaby.YamlDoc, output any, context yamlkit.VisitorContext, currentValue string) (any, error) {
		// TODO: Support this condition in the string visitor.
		if !strings.Contains(currentValue, yamlkit.PlaceHolderBlockApplyString) {
			return nil, nil
		}
		// We can't replace the placeholder string because reset doesn't restore the original
		// string, it replaces the whole field with the placeholder value. The whole new value
		// for each specific field is expected to be generated by the default name template.
		// Substrings should be targeted using embedded accessors.
		// Once the template strings are made extensible via API they will be easier to customize.
		// newValue := strings.ReplaceAll(currentValue, yamlkit.PlaceHolderBlockApplyString, defaultName)
		pathString := string(context.Path)
		_, err := doc.SetP(nameValue, pathString)
		return nil, errors.Wrap(err, "unable to set value of "+pathString)
	}
	nameConstructors := yamlkit.GetPathRegistryForAttributeName(resourceProvider, api.AttributeNameDefaultName)
	_, err := yamlkit.VisitPaths[string](parsedData, nameConstructors, []any{}, nil, resourceProvider, visitor, false)
	return parsedData, nil, err
}

func genericFnGetAttribute(resourceProvider yamlkit.ResourceProvider, _ *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, _ []byte) (gaby.Container, any, error) {
	attributeName := args[0].Value.(string)
	attributePaths := yamlkit.GetPathRegistryForAttributeName(resourceProvider, api.AttributeName(attributeName))
	if len(attributePaths) == 0 {
		return parsedData, nil, errors.New("attribute " + attributeName + " not registered")
	}
	values, err := yamlkit.GetPathsAnyType(parsedData, attributePaths, []any{}, resourceProvider, api.DataTypeNone, false)
	return parsedData, values, err
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
	return genericSetAttributesFromList(resourceProvider, functionContext, parsedData, attributeList, liveState)
}

func genericSetAttributesFromList(resourceProvider yamlkit.ResourceProvider, functionContext *api.FunctionContext, parsedData gaby.Container, attributeList api.AttributeValueList, liveState []byte) (gaby.Container, any, error) {
	var multiErrs []error
	for _, attribute := range attributeList {
		var err error
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
				parsedData, _, err = GenericFnSetStringPath(resourceProvider, functionContext, parsedData, setterArgs, liveState, false)
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
				parsedData, _, err = GenericFnSetIntPath(resourceProvider, functionContext, parsedData, setterArgs, liveState, false)
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
				parsedData, _, err = GenericFnSetBoolPath(resourceProvider, functionContext, parsedData, setterArgs, liveState, false)
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
			return parsedData, api.ValidationResultFalse, fmt.Errorf("failed to unmarshal data for config %s: %v", functionContext.UnitSlug, err)
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
			// Treat evaluation errors as expected and just fail the check, rather than parsing or expression errors.
			// There are many such error strings that the evaluator can return, so don't check them all.
			// Example prefixes:
			// "no such key:"
			// "index out of bounds:"
			// "no such attribute(s):"
			errorString := err.Error()
			details = append(details, "validation expression "+validationExpr+" could not be evaluated on resource "+string(resourceName)+": "+errorString)
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

func evaluateSplitPathExpressionWithComparators(expression *api.VisitorRelationalExpression, resourceType string, resourceProvider yamlkit.ResourceProvider, parsedData gaby.Container, customComparators []api.CustomStringComparator) (map[string]bool, error) {
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
			var err error
			matches, err = api.EvaluateExpression(&expression.RelationalExpression, value, nil, customComparators)
			if err != nil {
				return output, err
			}
		}

		if matches {
			if existingOutput, ok := output.(map[string]bool); ok {
				existingOutput[string(context.ResourceName)] = true
			}
		}

		return output, nil
	}

	_, err := yamlkit.VisitPathsDoc(parsedData, resourceTypeToPaths, []any{}, matchingResources, resourceProvider, visitor, false)
	if err != nil {
		return nil, err
	}

	return matchingResources, nil
}

func genericFnResourceWhereMatch(resourceProvider yamlkit.ResourceProvider, functionContext *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, liveState []byte) (gaby.Container, any, error) {
	return GenericFnResourceWhereMatchWithComparators(resourceProvider, nil, functionContext, parsedData, args, liveState)
}

func GenericFnResourceWhereMatchWithComparators(resourceProvider yamlkit.ResourceProvider, customComparators []api.CustomStringComparator, functionContext *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, liveState []byte) (gaby.Container, any, error) {
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

	expressions, err := api.ParseAndValidateWhereFilter(whereExpr)
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
			// Handle split path syntax with .| separator
			matchingResourcesForExpression, err := evaluateSplitPathExpressionWithComparators(expression, resourceType, resourceProvider, parsedData, customComparators)
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
				found, err := api.EvaluateExpression(&expression.RelationalExpression, attribValue.Value, nil, customComparators)
				if err != nil {
					multiErrs = append(multiErrs, err)
				} else if found {
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

func genericFnYQ(resourceProvider yamlkit.ResourceProvider, _ *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, _ []byte, mutating bool) (gaby.Container, any, error) {
	// The argument value types should be verified before this function is called
	expression := args[0].Value.(string)

	output, err := yamlkit.EvalYQExpression(expression, parsedData.String())
	if err != nil {
		return parsedData, nil, errors.Wrap(err, "yq expression evaluation failed")
	}
	if mutating {
		parsedOutput, err := gaby.ParseAll([]byte(output))
		if err != nil {
			return parsedData, nil, errors.Wrap(err, "failed to parse output from yq")
		}
		return parsedOutput, nil, nil
	}
	wrappedOutput := api.YAMLPayload{Payload: output}
	return parsedData, wrappedOutput, nil
}

func genericFnVetApprovedBy(resourceProvider yamlkit.ResourceProvider, functionContext *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, _ []byte) (gaby.Container, any, error) {
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
	upsert bool,
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
	reflector := jsonschema.Reflector{}
	attributeValueListSchema, err := reflector.Reflect(api.AttributeValueList{})
	if err != nil {
		log.Errorf("couldn't get schema for api.AttributeValueList")
	}
	// The getter output should match the last setter parameter
	outputInfo := &api.FunctionOutput{
		ResultName:  setterParameters[valueParameter].ParameterName,
		Description: setterParameters[valueParameter].Description,
		OutputType:  api.OutputTypeAttributeValueList,
		Schema:      &attributeValueListSchema,
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
			return genericFnSetStringVisitor(setterSignature, fc, c, fa, ls, resourceProvider, upsert)
		}
		getterFunction = func(fc *api.FunctionContext, c gaby.Container, fa []api.FunctionArgument, ls []byte) (gaby.Container, any, error) {
			return genericFnGetStringVisitor(getterSignature, fc, c, fa, ls, resourceProvider)
		}
	case api.DataTypeInt:
		setterFunction = func(fc *api.FunctionContext, c gaby.Container, fa []api.FunctionArgument, ls []byte) (gaby.Container, any, error) {
			return genericFnSetIntVisitor(setterSignature, fc, c, fa, ls, resourceProvider, upsert)
		}
		getterFunction = func(fc *api.FunctionContext, c gaby.Container, fa []api.FunctionArgument, ls []byte) (gaby.Container, any, error) {
			return genericFnGetIntVisitor(getterSignature, fc, c, fa, ls, resourceProvider)
		}
	case api.DataTypeBool:
		setterFunction = func(fc *api.FunctionContext, c gaby.Container, fa []api.FunctionArgument, ls []byte) (gaby.Container, any, error) {
			return genericFnSetBoolVisitor(setterSignature, fc, c, fa, ls, resourceProvider, upsert)
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

func genericFnSetStringVisitor(signature *api.FunctionSignature, _ *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, _ []byte, resourceProvider yamlkit.ResourceProvider, upsert bool) (gaby.Container, any, error) {
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
	err := yamlkit.UpdateStringPaths(parsedData, resourceTypeToPaths, pathArgs, resourceProvider, valueToSet, upsert)
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

func genericFnSetIntVisitor(signature *api.FunctionSignature, _ *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, _ []byte, resourceProvider yamlkit.ResourceProvider, upsert bool) (gaby.Container, any, error) {
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
	err := yamlkit.UpdatePathsValue[int](parsedData, resourceTypeToPaths, pathArgs, resourceProvider, valueToSet, upsert)
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

func genericFnSetBoolVisitor(signature *api.FunctionSignature, _ *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, _ []byte, resourceProvider yamlkit.ResourceProvider, upsert bool) (gaby.Container, any, error) {
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
	err := yamlkit.UpdatePathsValue[bool](parsedData, resourceTypeToPaths, pathArgs, resourceProvider, valueToSet, upsert)
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

func GenericFnDeletePath(resourceProvider yamlkit.ResourceProvider, _ *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, _ []byte) (gaby.Container, any, error) {
	// The argument value types should be verified before this function is called
	resourceType := args[0].Value.(string)
	unresolvedPath := args[1].Value.(string)

	resourceTypeToPaths := GetVisitorMapForPath(resourceProvider, api.ResourceType(resourceType), api.UnresolvedPath(unresolvedPath))
	err := yamlkit.DeletePaths(parsedData, resourceTypeToPaths, []any{}, resourceProvider)
	return parsedData, nil, err
}

func GenericFnVetJSONSchema(resourceProvider yamlkit.ResourceProvider, _ *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, _ []byte) (gaby.Container, any, error) {
	schemaMapJSON, ok := args[0].Value.(string)
	if !ok {
		return parsedData, api.ValidationResultFalse, errors.New("schema-map must be a string")
	}

	// Parse the schema map
	var schemaMap map[string]interface{}
	if err := json.Unmarshal([]byte(schemaMapJSON), &schemaMap); err != nil {
		return parsedData, api.ValidationResultFalse, errors.Wrap(err, "failed to parse schema-map JSON")
	}

	var multiErrs []error
	details := []string{}
	failedPaths := api.AttributeValueList{}
	passed := true

	// Use VisitResources to iterate over each document
	_, err := yamlkit.VisitResources(parsedData, nil, resourceProvider, func(doc *gaby.YamlDoc, output any, index int, resourceInfo *api.ResourceInfo) (any, []error) {
		var errs []error

		// Get the resource type
		resourceType := string(resourceInfo.ResourceType)

		// Look up the schema for this resource type
		schemaInterface, ok := schemaMap[resourceType]
		if !ok {
			// No schema for this resource type, skip validation
			return output, nil
		}

		// Convert schema to JSON string
		schemaBytes, err := json.Marshal(schemaInterface)
		if err != nil {
			errs = append(errs, errors.Wrapf(err, "failed to marshal schema for resource type %s", resourceType))
			return output, errs
		}

		// Marshal the document to JSON for validation
		docJSON, err := doc.MarshalJSON()
		if err != nil {
			errs = append(errs, errors.Wrapf(err, "failed to marshal document to JSON for resource %s/%s", resourceType, resourceInfo.ResourceName))
			return output, errs
		}

		// Create loaders for gojsonschema
		schemaLoader := gojsonschema.NewStringLoader(string(schemaBytes))
		documentLoader := gojsonschema.NewBytesLoader(docJSON)

		// Validate
		result, err := gojsonschema.Validate(schemaLoader, documentLoader)
		if err != nil {
			errs = append(errs, errors.Wrapf(err, "validation error for resource %s/%s", resourceType, resourceInfo.ResourceName))
			return output, errs
		}

		// Check validation result
		if !result.Valid() {
			passed = false
			for _, desc := range result.Errors() {
				detail := fmt.Sprintf("Resource %s/%s: %s", resourceType, resourceInfo.ResourceName, desc.String())
				details = append(details, detail)

				// Create a failed path entry
				// The field path from gojsonschema is in the format "(root).field.subfield"
				fieldPath := desc.Field()
				// Remove "(root)." prefix if present
				fieldPath = strings.TrimPrefix(fieldPath, "(root).")
				fieldPath = strings.TrimPrefix(fieldPath, "(root)")
				if fieldPath == "" {
					fieldPath = "."
				}

				// For required errors, get the missing property name and construct full path
				var failedValue interface{}
				failedValue = desc.Value()

				if desc.Type() == "required" {
					// Get the missing property name from details
					errorDetails := desc.Details()
					if property, ok := errorDetails["property"]; ok {
						if propertyName, ok := property.(string); ok {
							// Construct the full path to the missing field
							if fieldPath == "" || fieldPath == "." {
								fieldPath = propertyName
							} else {
								fieldPath = fieldPath + "." + propertyName
							}
							// Missing field has nil value
							failedValue = nil
						}
					}
				}

				failedPath := api.AttributeValue{
					AttributeInfo: api.AttributeInfo{
						AttributeIdentifier: api.AttributeIdentifier{
							ResourceInfo: *resourceInfo,
							Path:         api.ResolvedPath(fieldPath),
						},
						AttributeMetadata: api.AttributeMetadata{
							AttributeName: api.AttributeNameGeneral,
						},
					},
					Value: failedValue,
				}
				failedPaths = append(failedPaths, failedPath)
			}
		}

		return output, errs
	})

	if err != nil {
		multiErrs = append(multiErrs, err)
	}

	if passed && len(multiErrs) == 0 {
		return parsedData, api.ValidationResultTrue, nil
	}

	failureResult := api.ValidationResultFalse
	failureResult.Details = details
	failureResult.FailedAttributes = failedPaths

	return parsedData, failureResult, join.Join(multiErrs...)
}
