// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package generic

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"slices"

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

// attributeResources indexes the resources in the parsed data that an attribute can name, by
// position.
//
// The index is built from the data as it stands before any attribute is applied, and positions
// stay valid across the setters, which mutate resources in place without reordering them.
// Resolving names to positions up front is what lets an attribute reach the resource it names
// even after an earlier attribute in the same list renamed that resource — as search-replace
// does whenever the value it rewrites also appears in a resource's name.
type attributeResources struct {
	identifier api.ResourceIdentifier
	resources  []api.ResourceInfo
	positions  []int // positions[i] is the position in the parsed data of resources[i]
	index      *api.ResourceIndex
}

func indexAttributeResources(parsedData gaby.Container, resourceProvider yamlkit.ResourceProvider) *attributeResources {
	indexed := &attributeResources{identifier: resourceProvider}
	_, _ = yamlkit.VisitResources(parsedData, nil, resourceProvider,
		func(_ *gaby.YamlDoc, output any, position int, resourceInfo *api.ResourceInfo) (any, []error) {
			if resourceInfo.ResourceName != "" {
				indexed.resources = append(indexed.resources, *resourceInfo)
				indexed.positions = append(indexed.positions, position)
			}
			return output, nil
		})
	indexed.index = api.NewResourceIndex(resourceProvider, indexed.resources)
	return indexed
}

// scopeOptionsToResource returns options restricted to the resource an attribute names,
// so that the attribute is set only on that resource rather than on every resource of its
// type. Returns options unchanged when the attribute names no resource.
//
// The resource is found as api.ResourceIndex finds one: by its type, in any API version, and
// its name, with its scope or, when that names one resource, without it. So an attribute
// recorded before a resource's namespace changed -- a Link's DownstreamPaths after a clone and
// set-namespace, say -- still reaches it, and an attribute typed for one of two same-named
// resources doesn't select the other. The setters apply their own resource-type filter, which
// would mask an over-broad selection here; scoping on both keeps the index set meaningful on
// its own. An attribute typed ResourceTypeAny names a resource of any type, so it selects
// every resource carrying the name.
//
// An attribute naming a resource that isn't in the data, or a name that matches resources in
// several namespaces without its own, is an error rather than a write to nothing or to
// everything: the caller named a resource this configuration doesn't identify, and a write
// that silently lands nowhere leaves the caller believing it landed.
func scopeOptionsToResource(resources *attributeResources, options *api.FunctionOptions, resourceType api.ResourceType, resourceName api.ResourceName) (*api.FunctionOptions, error) {
	if resourceName == "" {
		return options, nil
	}
	scoped := api.FunctionOptions{}
	if options != nil {
		scoped = *options
	}
	scoped.ResourceIndexes = []int{}

	if resourceType != api.ResourceTypeAny {
		match := resources.index.Match(api.ResourceInfo{ResourceType: resourceType, ResourceName: resourceName})
		if len(match.Ambiguous) > 0 {
			return nil, fmt.Errorf("%s %s matches several resources in different scopes: %v", resourceType, resourceName, match.Ambiguous)
		}
		if !match.Found {
			return nil, fmt.Errorf("the configuration has no %s named %s", resourceType, resourceName)
		}
		scoped.ResourceIndexes = append(scoped.ResourceIndexes, resources.positions[match.Position])
		return &scoped, nil
	}

	// Any type: every resource carrying the name, or carrying it without its scope when those
	// are all in one scope.
	nameWithoutScope := resources.identifier.RemoveScopeFromResourceName(resourceName)
	var exact, unscoped []int
	var unscopedNames []api.ResourceName
	for i := range resources.resources {
		resource := &resources.resources[i]
		if resource.ResourceName == resourceName {
			exact = append(exact, resources.positions[i])
		}
		if api.ResourceNameWithoutScopeFromResourceInfo(resources.identifier, *resource) == nameWithoutScope {
			unscoped = append(unscoped, resources.positions[i])
			if !slices.Contains(unscopedNames, resource.ResourceName) {
				unscopedNames = append(unscopedNames, resource.ResourceName)
			}
		}
	}
	switch {
	case len(exact) > 0:
		scoped.ResourceIndexes = exact
	case len(unscopedNames) == 1:
		scoped.ResourceIndexes = unscoped
	case len(unscopedNames) > 1:
		slices.Sort(unscopedNames)
		return nil, fmt.Errorf("%s matches resources in different scopes: %v", resourceName, unscopedNames)
	default:
		return nil, fmt.Errorf("the configuration has no resource named %s", resourceName)
	}
	return &scoped, nil
}

func genericSetAttributesFromList(resourceProvider yamlkit.ResourceProvider, functionContext *api.FunctionContext, parsedData gaby.Container, attributeList api.AttributeValueList, options *api.FunctionOptions) (gaby.Container, any, error) {
	var multiErrs []error
	resources := indexAttributeResources(parsedData, resourceProvider)
	for _, attribute := range attributeList {
		var err error
		setterArgs := make([]api.FunctionArgument, 3)
		setterArgs[0].Value = string(attribute.ResourceType)
		setterArgs[1].Value = string(attribute.Path)
		attributeOptions, err := scopeOptionsToResource(resources, options, attribute.ResourceType, attribute.ResourceName)
		if err != nil {
			multiErrs = append(multiErrs, fmt.Errorf("attribute %s at %s: %w", attribute.AttributeName, attribute.Path, err))
			continue
		}
		if err := checkAttributePathResolves(parsedData, attributeOptions, attribute); err != nil {
			multiErrs = append(multiErrs, err)
			continue
		}
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

// checkAttributePathResolves reports an attribute that names a resource and a path the setter
// cannot write in it. The setters skip a path they cannot resolve, which is right for a path
// applied across resources of a type, where most may not have it; but an attribute that names
// one resource asks for that path in that resource, and a merge-key segment matching no element
// there -- `containers.?name=sidecar.image` with no sidecar -- would otherwise be a write to
// nowhere reported as success.
func checkAttributePathResolves(parsedData gaby.Container, options *api.FunctionOptions, attribute api.AttributeValue) error {
	if attribute.ResourceName == "" || options == nil {
		return nil
	}
	for _, position := range options.ResourceIndexes {
		if position < 0 || position >= len(parsedData) {
			continue
		}
		if err := yamlkit.CheckPathSettable(parsedData[position], api.UnresolvedPath(attribute.Path)); err != nil {
			return fmt.Errorf("attribute %s in %s %s: %w", attribute.AttributeName, attribute.ResourceType, attribute.ResourceName, err)
		}
	}
	return nil
}
