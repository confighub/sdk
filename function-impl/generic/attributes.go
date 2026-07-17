// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package generic

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"math"

	"github.com/cockroachdb/errors"
	"github.com/cockroachdb/errors/join"
	"github.com/confighub/sdk/core/configkit"
	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/function/handler"
	"github.com/confighub/sdk/core/third_party/gaby"
)

func registerGetAttribute(fh handler.FunctionRegistry, converter configkit.ConfigConverter, resourceProvider yamlkit.ResourceProvider) {
	if err := fh.RegisterFunction("get-attribute", &handler.FunctionRegistration{
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
				{
					ParameterName: "key",
					Required:      false,
					Description:   "Path parameters (keys) used to resolve the attribute's registered paths, in order. Required only for attributes whose paths contain associative selectors, such as the container name and env var name for the env-value attribute.",
					DataType:      api.DataTypeString,
					Example:       "main",
				},
			},
			VarArgs: true,
			OutputInfo: &api.FunctionOutput{
				ResultName:  "attribute-list",
				Description: "Specified attribute values",
				OutputType:  api.OutputTypeAttributeValueList,
				Schema:      &api.AttributeValueListSchema,
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
		Function: func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
			return genericFnGetAttribute(resourceProvider, fArgs.FunctionContext, fArgs.ParsedData, fArgs.Arguments, fArgs.Options)
		},
	}); err != nil {
		slog.Error("failed to register function", "error", err)
	}
}

func genericFnGetAttribute(resourceProvider yamlkit.ResourceProvider, _ *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, options *api.FunctionOptions) (gaby.Container, any, error) {
	attributeName := args[0].Value.(string)
	attributePaths := yamlkit.GetPathRegistryForAttributeName(resourceProvider, api.AttributeName(attributeName))
	if len(attributePaths) == 0 {
		return parsedData, nil, errors.New("attribute " + attributeName + " not registered")
	}
	// Remaining arguments are path parameters (keys) used to resolve associative
	// selectors in the attribute's registered paths, e.g. the container name and
	// env var name for the env-value attribute. These are matched as values, not
	// map key path segments, so dots are not escaped.
	keyArgs := args[1:]
	keys := make([]any, len(keyArgs))
	for i := range keyArgs {
		key, ok := keyArgs[i].Value.(string)
		if !ok {
			return parsedData, nil, errors.New("invalid keys argument")
		}
		keys[i] = key
	}
	values, err := yamlkit.GetPathsAnyType(parsedData, attributePaths, keys, resourceProvider, api.DataTypeNone, false, false, options)
	return parsedData, values, err
}

func registerSetAttributes(fh handler.FunctionRegistry, converter configkit.ConfigConverter, resourceProvider yamlkit.ResourceProvider) {
	if err := fh.RegisterFunction("set-attributes", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "set-attributes",
			Parameters: []api.FunctionParameter{
				{
					ParameterName:    "attribute-list",
					Required:         true,
					Description:      "List of attributes to set",
					DataType:         api.DataTypeAttributeValueList,
					ValueConstraints: api.ValueConstraints{Schema: &api.AttributeValueListSchema},
				},
			},
			Mutating:              true,
			Validating:            false,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Set specified attributes to the specified values. Each attribute is set on the resource named by its ResourceName; an attribute with no ResourceName is set on every resource of its ResourceType. This function is intended to be used for read-modify-write operations in combination with any (typically `get-`) functions returning output of the type " + string(api.DataTypeAttributeValueList) + ".",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
			return genericFnSetAttributes(resourceProvider, fArgs.FunctionContext, fArgs.ParsedData, fArgs.Arguments, fArgs.Options)
		},
	}); err != nil {
		slog.Error("failed to register function", "error", err)
	}
}

func genericFnSetAttributes(resourceProvider yamlkit.ResourceProvider, functionContext *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, options *api.FunctionOptions) (gaby.Container, any, error) {
	attributeListString := args[0].Value.(string)
	var attributeList api.AttributeValueList
	err := json.Unmarshal([]byte(attributeListString), &attributeList)
	if err != nil {
		return parsedData, nil, err
	}
	return genericSetAttributesFromList(resourceProvider, functionContext, parsedData, attributeList, options)
}

// indexedResource is a resource's position in the parsed data together with its type,
// which tells apart resources that share a name — a Deployment and a Service both named
// default/web, say.
type indexedResource struct {
	Index        int
	ResourceType api.ResourceType
}

// resourceIndexesByName maps each resource's name to the resources carrying that name.
//
// The map is built from the data as it stands before any attribute is applied, and indexes
// stay valid across the setters, which mutate resources in place without reordering them.
// Resolving names to indexes up front is what lets an attribute reach the resource it names
// even after an earlier attribute in the same list renamed that resource — as search-replace
// does whenever the value it rewrites also appears in a resource's name.
func resourceIndexesByName(parsedData gaby.Container, resourceProvider yamlkit.ResourceProvider) map[api.ResourceName][]indexedResource {
	indexes := map[api.ResourceName][]indexedResource{}
	_, _ = yamlkit.VisitResources(parsedData, nil, resourceProvider,
		func(_ *gaby.YamlDoc, output any, index int, resourceInfo *api.ResourceInfo) (any, []error) {
			if resourceInfo.ResourceName != "" {
				indexes[resourceInfo.ResourceName] = append(indexes[resourceInfo.ResourceName],
					indexedResource{Index: index, ResourceType: resourceInfo.ResourceType})
			}
			return output, nil
		})
	return indexes
}

// scopeOptionsToResource returns options restricted to the resource an attribute names,
// so that the attribute is set only on that resource rather than on every resource of its
// type. Returns options unchanged when the attribute names no resource.
//
// resourceType is matched as well as the name, so an attribute typed for one of two
// same-named resources doesn't select the other. The setters apply their own resource-type
// filter, which would mask an over-broad selection here; scoping on both keeps the index
// set meaningful on its own. An attribute typed ResourceTypeAny names a resource of any
// type, so it selects every resource carrying the name.
//
// An attribute naming a resource that isn't in the data is scoped to no resources rather
// than to all of them: the caller named a resource this Unit doesn't have, so the safe
// reading is that nothing should be written.
func scopeOptionsToResource(options *api.FunctionOptions, indexesByName map[api.ResourceName][]indexedResource, resourceType api.ResourceType, resourceName api.ResourceName) *api.FunctionOptions {
	if resourceName == "" {
		return options
	}
	scoped := api.FunctionOptions{}
	if options != nil {
		scoped = *options
	}
	scoped.ResourceIndexes = []int{}
	for _, resource := range indexesByName[resourceName] {
		if resourceType == api.ResourceTypeAny || resource.ResourceType == resourceType {
			scoped.ResourceIndexes = append(scoped.ResourceIndexes, resource.Index)
		}
	}
	return &scoped
}

func genericSetAttributesFromList(resourceProvider yamlkit.ResourceProvider, functionContext *api.FunctionContext, parsedData gaby.Container, attributeList api.AttributeValueList, options *api.FunctionOptions) (gaby.Container, any, error) {
	var multiErrs []error
	indexesByName := resourceIndexesByName(parsedData, resourceProvider)
	for _, attribute := range attributeList {
		var err error
		setterArgs := make([]api.FunctionArgument, 3)
		setterArgs[0].Value = string(attribute.ResourceType)
		setterArgs[1].Value = string(attribute.Path)
		attributeOptions := scopeOptionsToResource(options, indexesByName, attribute.ResourceType, attribute.ResourceName)
		switch attribute.DataType {
		case api.DataTypeString:
			stringValue, ok := attribute.Value.(string)
			if !ok {
				multiErrs = append(multiErrs, fmt.Errorf("value of attribute %s is not string: %v", attribute.AttributeName, attribute.Value))
			} else {
				setterArgs[2].Value = stringValue
				parsedData, _, err = GenericFnSetStringPath(resourceProvider, functionContext, parsedData, setterArgs, true, attributeOptions)
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
				parsedData, _, err = GenericFnSetIntPath(resourceProvider, functionContext, parsedData, setterArgs, true, attributeOptions)
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
				parsedData, _, err = GenericFnSetBoolPath(resourceProvider, functionContext, parsedData, setterArgs, true, attributeOptions)
				if err != nil {
					multiErrs = append(multiErrs, err)
				}
			}
		case api.DataTypeYAML:
			// A whole document (sub-tree) value. The converted format is YAML, so
			// JSON is accepted as a subset. Replaces all fields at the path; a
			// terminal ?key=value segment find-or-appends a merge-keyed element.
			yamlValue, ok := attribute.Value.(string)
			if !ok {
				multiErrs = append(multiErrs, fmt.Errorf("value of attribute %s is not a YAML string: %v", attribute.AttributeName, attribute.Value))
			} else {
				setterArgs[2].Value = yamlValue
				parsedData, _, err = GenericFnSetPath(resourceProvider, functionContext, parsedData, setterArgs, true, attributeOptions)
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
	if err := fh.RegisterFunction("get-details", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "get-details",
			OutputInfo: &api.FunctionOutput{
				ResultName:  "attribute-list",
				Description: "Selected resource attributes",
				OutputType:  api.OutputTypeAttributeValueList,
				Schema:      &api.AttributeValueListSchema,
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
		Function: func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
			return genericFnGetDetails(resourceProvider, fArgs.FunctionContext, fArgs.ParsedData, fArgs.Arguments, fArgs.Options)
		},
	}); err != nil {
		slog.Error("failed to register function", "error", err)
	}
}

func genericFnGetDetails(resourceProvider yamlkit.ResourceProvider, _ *api.FunctionContext, parsedData gaby.Container, _ []api.FunctionArgument, options *api.FunctionOptions) (gaby.Container, any, error) {
	detailPaths := yamlkit.GetPathRegistryForAttributeName(resourceProvider, api.AttributeNameDetail)
	values, err := yamlkit.GetPathsAnyType(parsedData, detailPaths, []any{}, resourceProvider, api.DataTypeNone, false, false, options)
	return parsedData, values, err
}
