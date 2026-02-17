// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package generic

import (
	"encoding/json"
	"fmt"
	"math"

	"github.com/cockroachdb/errors"
	"github.com/cockroachdb/errors/join"
	"github.com/confighub/sdk/configkit"
	"github.com/confighub/sdk/configkit/yamlkit"
	"github.com/confighub/sdk/function/api"
	"github.com/confighub/sdk/function/handler"
	"github.com/confighub/sdk/third_party/gaby"
)

func registerGetAttribute(fh handler.FunctionRegistry, converter configkit.ConfigConverter, resourceProvider yamlkit.ResourceProvider) {
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
		Function: func(functionContext *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument) (gaby.Container, any, error) {
			return genericFnGetAttribute(resourceProvider, functionContext, parsedData, args)
		},
	})
}

func genericFnGetAttribute(resourceProvider yamlkit.ResourceProvider, _ *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument) (gaby.Container, any, error) {
	attributeName := args[0].Value.(string)
	attributePaths := yamlkit.GetPathRegistryForAttributeName(resourceProvider, api.AttributeName(attributeName))
	if len(attributePaths) == 0 {
		return parsedData, nil, errors.New("attribute " + attributeName + " not registered")
	}
	values, err := yamlkit.GetPathsAnyType(parsedData, attributePaths, []any{}, resourceProvider, api.DataTypeNone, false)
	return parsedData, values, err
}

func registerGetAttributes(fh handler.FunctionRegistry, converter configkit.ConfigConverter, resourceProvider yamlkit.ResourceProvider) {
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
		Function: func(functionContext *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument) (gaby.Container, any, error) {
			return genericFnGetAttributes(resourceProvider, functionContext, parsedData, args)
		},
	})
}

func genericFnGetAttributes(resourceProvider yamlkit.ResourceProvider, _ *api.FunctionContext, parsedData gaby.Container, _ []api.FunctionArgument) (gaby.Container, any, error) {
	attributePaths := yamlkit.GetPathRegistryForAttributeName(resourceProvider, api.AttributeNameGeneral)
	values, err := yamlkit.GetPathsAnyType(parsedData, attributePaths, []any{}, resourceProvider, api.DataTypeNone, false)
	return parsedData, values, err
}

func registerSetAttributes(fh handler.FunctionRegistry, converter configkit.ConfigConverter, resourceProvider yamlkit.ResourceProvider) {
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
		Function: func(functionContext *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument) (gaby.Container, any, error) {
			return genericFnSetAttributes(resourceProvider, functionContext, parsedData, args)
		},
	})
}

func genericFnSetAttributes(resourceProvider yamlkit.ResourceProvider, functionContext *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument) (gaby.Container, any, error) {
	attributeListString := args[0].Value.(string)
	var attributeList api.AttributeValueList
	err := json.Unmarshal([]byte(attributeListString), &attributeList)
	if err != nil {
		return parsedData, nil, err
	}
	return genericSetAttributesFromList(resourceProvider, functionContext, parsedData, attributeList)
}

func genericSetAttributesFromList(resourceProvider yamlkit.ResourceProvider, functionContext *api.FunctionContext, parsedData gaby.Container, attributeList api.AttributeValueList) (gaby.Container, any, error) {
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
				parsedData, _, err = GenericFnSetStringPath(resourceProvider, functionContext, parsedData, setterArgs, false)
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
				parsedData, _, err = GenericFnSetIntPath(resourceProvider, functionContext, parsedData, setterArgs, false)
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
				parsedData, _, err = GenericFnSetBoolPath(resourceProvider, functionContext, parsedData, setterArgs, false)
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

func registerGetDetails(fh handler.FunctionRegistry, converter configkit.ConfigConverter, resourceProvider yamlkit.ResourceProvider) {
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
		Function: func(functionContext *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument) (gaby.Container, any, error) {
			return genericFnGetDetails(resourceProvider, functionContext, parsedData, args)
		},
	})
}

func genericFnGetDetails(resourceProvider yamlkit.ResourceProvider, _ *api.FunctionContext, parsedData gaby.Container, _ []api.FunctionArgument) (gaby.Container, any, error) {
	detailPaths := yamlkit.GetPathRegistryForAttributeName(resourceProvider, api.AttributeNameDetail)
	values, err := yamlkit.GetPathsAnyType(parsedData, detailPaths, []any{}, resourceProvider, api.DataTypeNone, false)
	return parsedData, values, err
}
