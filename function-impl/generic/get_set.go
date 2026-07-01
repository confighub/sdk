// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package generic

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/confighub/sdk/core/configkit"
	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/function/handler"
	"github.com/confighub/sdk/core/third_party/gaby"
)

func registerGetPath(fh handler.FunctionRegistry, converter configkit.ConfigConverter, resourceProvider yamlkit.ResourceProvider) {
	if err := fh.RegisterFunction("get-path", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "get-path",
			Parameters: []api.FunctionParameter{
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
				Schema:      &api.AttributeValueListSchema,
			},
			Mutating:              false,
			Validating:            false,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Returns the value(s) of the specified attribute path of any type",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
			return GenericFnGetPath(resourceProvider, fArgs.FunctionContext, fArgs.ParsedData, fArgs.Arguments, fArgs.Options)
		},
	}); err != nil {
		slog.Error("failed to register function", "error", err)
	}
}

func GenericFnGetPath(resourceProvider yamlkit.ResourceProvider, _ *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, options *api.FunctionOptions) (gaby.Container, any, error) {
	// The argument value types should be verified before this function is called
	unresolvedPath := args[0].Value.(string)

	resourceTypeToPaths := yamlkit.GetVisitorMapForPath(resourceProvider, api.ResourceTypeAny, api.UnresolvedPath(unresolvedPath))
	values, err := yamlkit.GetPathsAnyType(parsedData, resourceTypeToPaths, []any{}, resourceProvider, api.DataTypeNone, false, false, options)
	return parsedData, values, err
}

// registerSetPath registers set-path, the untyped counterpart to get-path: it sets
// the document (a YAML value) at a path, replacing all fields at that path
// ("upsert" semantics). Combined with an associative path segment
// (?key=value), it find-or-appends an element in a merge-keyed list — e.g.
// injecting a sidecar container into spec.template.spec.containers.?name=otel.
func registerSetPath(fh handler.FunctionRegistry, converter configkit.ConfigConverter, resourceProvider yamlkit.ResourceProvider) {
	if err := fh.RegisterFunction("set-path", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "set-path",
			Parameters: []api.FunctionParameter{
				{
					ParameterName:    "path",
					Required:         true,
					Description:      "Dot-separated configuration path at which to set the document. A terminal associative segment (?key=value) find-or-appends an element in a merge-keyed list. See https://docs.confighub.com/guide/functions/#configuration-path-syntax for path syntax.",
					DataType:         api.DataTypeString,
					ValueConstraints: api.ValueConstraints{Regexp: api.PathRegexpString},
				},
				{
					ParameterName: "value",
					Required:      true,
					Description:   "YAML document to set at the path. Replaces all fields at that path (not a merge). JSON is accepted as a subset of YAML.",
					DataType:      api.DataTypeYAML,
				},
			},
			Mutating:              true,
			Validating:            false,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Set the YAML document at the specified path, replacing all fields there; a terminal ?key=value segment find-or-appends a merge-keyed list element",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
			// Prepend ResourceTypeAny so GenericFnSetPath shares the
			// (resource-type, path, value) arg layout of the typed setters.
			setterArgs := []api.FunctionArgument{
				{Value: string(api.ResourceTypeAny)},
				fArgs.Arguments[0],
				fArgs.Arguments[1],
			}
			return GenericFnSetPath(resourceProvider, fArgs.FunctionContext, fArgs.ParsedData, setterArgs, true, fArgs.Options)
		},
	}); err != nil {
		slog.Error("failed to register function", "error", err)
	}
}

// GenericFnSetPath sets a YAML document value at args[1] (path) for resource type
// args[0], with args[2] the YAML value. It replaces all fields at the path; with
// a terminal associative segment it find-or-appends a merge-keyed list element,
// injecting the merge key into the value when the value omits it. Also used by
// set-attributes for DataTypeYAML attributes.
func GenericFnSetPath(resourceProvider yamlkit.ResourceProvider, _ *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, upsert bool, options *api.FunctionOptions) (gaby.Container, any, error) {
	resourceType := args[0].Value.(string)
	unresolvedPath := args[1].Value.(string)
	valueYAML, ok := args[2].Value.(string)
	if !ok {
		return parsedData, nil, fmt.Errorf("set-path value must be a YAML string, got %T", args[2].Value)
	}

	valueDoc, err := gaby.ParseYAML([]byte(valueYAML))
	if err != nil {
		return parsedData, nil, errors.Wrapf(err, "parsing YAML value for path %s", unresolvedPath)
	}

	// Inject the terminal merge key so a replaced or appended element carries its
	// key even when the value omits it (e.g. set-path ...containers.?name=otel
	// value='{image: x}' yields {name: otel, image: x}). Only when the path ends
	// at an associative segment (a whole-element set), not for field edits.
	segments := gaby.DotPathToSlice(unresolvedPath)
	if len(segments) > 0 && strings.HasPrefix(segments[len(segments)-1], "?") {
		mergeKeys := yamlkit.ExtractMergeKeysFromPath(unresolvedPath)
		if len(mergeKeys) > 0 {
			mk := mergeKeys[len(mergeKeys)-1]
			if valueDoc.S(mk.Key) == nil {
				if _, err := valueDoc.SetP(mk.Value, mk.Key); err != nil {
					return parsedData, nil, errors.Wrapf(err, "injecting merge key %s at path %s", mk.Key, unresolvedPath)
				}
			}
		}
	}

	// Serialize the (possibly key-injected) value once and re-parse per matched
	// path so multiple matches (e.g. a wildcard) don't share the same YAML node.
	injectedYAML := valueDoc.String()
	resourceTypeToPaths := yamlkit.GetVisitorMapForPath(resourceProvider, api.ResourceType(resourceType), api.UnresolvedPath(unresolvedPath))
	err = yamlkit.UpdatePathsFunctionDoc(parsedData, resourceTypeToPaths, []any{}, resourceProvider,
		func(_ *gaby.YamlDoc, _ yamlkit.VisitorContext) *gaby.YamlDoc {
			d, parseErr := gaby.ParseYAML([]byte(injectedYAML))
			if parseErr != nil {
				return valueDoc
			}
			return d
		},
		upsert, options)
	return parsedData, nil, err
}

func registerGetStringPath(fh handler.FunctionRegistry, converter configkit.ConfigConverter, resourceProvider yamlkit.ResourceProvider) {
	if err := fh.RegisterFunction("get-string-path", &handler.FunctionRegistration{
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
				Schema:      &api.AttributeValueListSchema,
			},
			Mutating:              false,
			Validating:            false,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Returns the value(s) of the specified attribute path",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
			return GenericFnGetStringPath(resourceProvider, fArgs.FunctionContext, fArgs.ParsedData, fArgs.Arguments, fArgs.Options)
		},
	}); err != nil {
		slog.Error("failed to register function", "error", err)
	}
}

func GenericFnGetStringPath(resourceProvider yamlkit.ResourceProvider, _ *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, options *api.FunctionOptions) (gaby.Container, any, error) {
	// The argument value types should be verified before this function is called
	resourceType := args[0].Value.(string)
	unresolvedPath := args[1].Value.(string)

	resourceTypeToPaths := yamlkit.GetVisitorMapForPath(resourceProvider, api.ResourceType(resourceType), api.UnresolvedPath(unresolvedPath))
	values, err := yamlkit.GetStringPaths(parsedData, resourceTypeToPaths, []any{}, resourceProvider, options)
	return parsedData, values, err
}

func registerSetStringPath(fh handler.FunctionRegistry, converter configkit.ConfigConverter, resourceProvider yamlkit.ResourceProvider) {
	if err := fh.RegisterFunction("set-string-path", &handler.FunctionRegistration{
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
		Function: func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
			return GenericFnSetStringPath(resourceProvider, fArgs.FunctionContext, fArgs.ParsedData, fArgs.Arguments, true, fArgs.Options)
		},
	}); err != nil {
		slog.Error("failed to register function", "error", err)
	}
}

func GenericFnSetStringPath(resourceProvider yamlkit.ResourceProvider, _ *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, upsert bool, options *api.FunctionOptions) (gaby.Container, any, error) {
	// The argument value types should be verified before this function is called
	resourceType := args[0].Value.(string)
	unresolvedPath := args[1].Value.(string)
	value := args[2].Value.(string)

	resourceTypeToPaths := yamlkit.GetVisitorMapForPath(resourceProvider, api.ResourceType(resourceType), api.UnresolvedPath(unresolvedPath))
	err := yamlkit.UpdateStringPaths(parsedData, resourceTypeToPaths, []any{}, resourceProvider, value, upsert, options)
	return parsedData, nil, err
}

func registerGetIntPath(fh handler.FunctionRegistry, converter configkit.ConfigConverter, resourceProvider yamlkit.ResourceProvider) {
	if err := fh.RegisterFunction("get-int-path", &handler.FunctionRegistration{
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
				Schema:      &api.AttributeValueListSchema,
			},
			Mutating:              false,
			Validating:            false,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Returns the value(s) of the specified attribute path",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
			return GenericFnGetIntPath(resourceProvider, fArgs.FunctionContext, fArgs.ParsedData, fArgs.Arguments, fArgs.Options)
		},
	}); err != nil {
		slog.Error("failed to register function", "error", err)
	}
}

func GenericFnGetIntPath(resourceProvider yamlkit.ResourceProvider, _ *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, options *api.FunctionOptions) (gaby.Container, any, error) {
	// The argument value types should be verified before this function is called
	resourceType := args[0].Value.(string)
	unresolvedPath := args[1].Value.(string)

	resourceTypeToPaths := yamlkit.GetVisitorMapForPath(resourceProvider, api.ResourceType(resourceType), api.UnresolvedPath(unresolvedPath))
	values, err := yamlkit.GetPaths[int](parsedData, resourceTypeToPaths, []any{}, resourceProvider, options)
	return parsedData, values, err
}

func registerSetIntPath(fh handler.FunctionRegistry, converter configkit.ConfigConverter, resourceProvider yamlkit.ResourceProvider) {
	if err := fh.RegisterFunction("set-int-path", &handler.FunctionRegistration{
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
		Function: func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
			return GenericFnSetIntPath(resourceProvider, fArgs.FunctionContext, fArgs.ParsedData, fArgs.Arguments, true, fArgs.Options)
		},
	}); err != nil {
		slog.Error("failed to register function", "error", err)
	}
}

func GenericFnSetIntPath(resourceProvider yamlkit.ResourceProvider, _ *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, upsert bool, options *api.FunctionOptions) (gaby.Container, any, error) {
	// The argument value types should be verified before this function is called
	resourceType := args[0].Value.(string)
	unresolvedPath := args[1].Value.(string)
	value := args[2].Value.(int)

	resourceTypeToPaths := yamlkit.GetVisitorMapForPath(resourceProvider, api.ResourceType(resourceType), api.UnresolvedPath(unresolvedPath))
	err := yamlkit.UpdatePathsValue[int](parsedData, resourceTypeToPaths, []any{}, resourceProvider, value, upsert, options)
	return parsedData, nil, err
}

func registerGetBoolPath(fh handler.FunctionRegistry, converter configkit.ConfigConverter, resourceProvider yamlkit.ResourceProvider) {
	if err := fh.RegisterFunction("get-bool-path", &handler.FunctionRegistration{
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
				Schema:      &api.AttributeValueListSchema,
			},
			Mutating:              false,
			Validating:            false,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Returns the value(s) of the specified attribute path",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
			return GenericFnGetBoolPath(resourceProvider, fArgs.FunctionContext, fArgs.ParsedData, fArgs.Arguments, fArgs.Options)
		},
	}); err != nil {
		slog.Error("failed to register function", "error", err)
	}
}

func GenericFnGetBoolPath(resourceProvider yamlkit.ResourceProvider, _ *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, options *api.FunctionOptions) (gaby.Container, any, error) {
	// The argument value types should be verified before this function is called
	resourceType := args[0].Value.(string)
	unresolvedPath := args[1].Value.(string)

	resourceTypeToPaths := yamlkit.GetVisitorMapForPath(resourceProvider, api.ResourceType(resourceType), api.UnresolvedPath(unresolvedPath))
	values, err := yamlkit.GetPaths[bool](parsedData, resourceTypeToPaths, []any{}, resourceProvider, options)
	return parsedData, values, err
}

func registerSetBoolPath(fh handler.FunctionRegistry, converter configkit.ConfigConverter, resourceProvider yamlkit.ResourceProvider) {
	if err := fh.RegisterFunction("set-bool-path", &handler.FunctionRegistration{
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
		Function: func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
			return GenericFnSetBoolPath(resourceProvider, fArgs.FunctionContext, fArgs.ParsedData, fArgs.Arguments, true, fArgs.Options)
		},
	}); err != nil {
		slog.Error("failed to register function", "error", err)
	}
}

func GenericFnSetBoolPath(resourceProvider yamlkit.ResourceProvider, _ *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, upsert bool, options *api.FunctionOptions) (gaby.Container, any, error) {
	// The argument value types should be verified before this function is called
	resourceType := args[0].Value.(string)
	unresolvedPath := args[1].Value.(string)
	value := args[2].Value.(bool)

	resourceTypeToPaths := yamlkit.GetVisitorMapForPath(resourceProvider, api.ResourceType(resourceType), api.UnresolvedPath(unresolvedPath))
	err := yamlkit.UpdatePathsValue[bool](parsedData, resourceTypeToPaths, []any{}, resourceProvider, value, upsert, options)
	return parsedData, nil, err
}

func registerSetPathComment(fh handler.FunctionRegistry, converter configkit.ConfigConverter, resourceProvider yamlkit.ResourceProvider) {
	if err := fh.RegisterFunction("set-path-comment", &handler.FunctionRegistration{
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
		Function: func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
			return genericFnSetPathComment(resourceProvider, fArgs.FunctionContext, fArgs.ParsedData, fArgs.Arguments, fArgs.Options)
		},
	}); err != nil {
		slog.Error("failed to register function", "error", err)
	}
}

func genericFnSetPathComment(resourceProvider yamlkit.ResourceProvider, _ *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, options *api.FunctionOptions) (gaby.Container, any, error) {
	// The argument value types should be verified before this function is called
	resourceType := args[0].Value.(string)
	unresolvedPath := args[1].Value.(string)
	comment := args[2].Value.(string)

	resourceTypeToPaths := yamlkit.GetVisitorMapForPath(resourceProvider, api.ResourceType(resourceType), api.UnresolvedPath(unresolvedPath))
	visitor := func(doc *gaby.YamlDoc, output any, context yamlkit.VisitorContext, currentDoc *gaby.YamlDoc) (any, error) {
		// Set the comment as a $comment$line$ sibling key in the parent map
		err := doc.SetCommentKey(string(context.Path), "line", comment)
		return output, err
	}
	_, err := yamlkit.VisitPathsDoc(parsedData, resourceTypeToPaths, []any{}, nil, resourceProvider, visitor, false, options)
	return parsedData, nil, err
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
	defaults bool,
) {
	if len(parameters) == 0 && !defaults {
		// No parameters or output value information were provided, so don't register either the getter or the setter
		return
	}

	resourceTypes := yamlkit.ResourceTypesForAttribute(attributeName, resourceProvider)

	if defaults {
		// Defaults mode: getter returns any type, setter takes no value parameter,
		// and a vet- function validates current values match defaults.
		// Path parameters are all parameters (defaults mode has no value parameter).
		pathParameters := make([]api.FunctionParameter, len(parameters))
		copy(pathParameters, parameters)

		// Getter: takes only path parameters, returns any type
		getterParameters := make([]api.FunctionParameter, len(parameters))
		for i := range getterParameters {
			getterParameters[i] = parameters[i]
			getterParameters[i].Description += "get"
		}
		outputInfo := &api.FunctionOutput{
			ResultName:  name,
			Description: description,
			OutputType:  api.OutputTypeAttributeValueList,
			Schema:      &api.AttributeValueListSchema,
		}
		getterSignature := &api.FunctionSignature{
			FunctionName:          "get-" + name,
			Parameters:            getterParameters,
			OutputInfo:            outputInfo,
			Mutating:              false,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Get" + description,
			FunctionType:          api.FunctionTypePathVisitor,
			AttributeName:         attributeName,
			AffectedResourceTypes: resourceTypes,
		}
		if err := fh.RegisterFunction("get-"+name, &handler.FunctionRegistration{
			FunctionSignature: *getterSignature,
			Function: func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
				return genericFnGetVisitorAnyType(getterSignature, fArgs.FunctionContext, fArgs.ParsedData, fArgs.Arguments, resourceProvider, fArgs.Options)
			},
		}); err != nil {
			slog.Error("failed to register function", "error", err)
		}

		// Setter: takes only path parameters, uses UpdatePathsSetterArgument
		if addSetter {
			setterParameters := make([]api.FunctionParameter, len(parameters))
			for i := range setterParameters {
				setterParameters[i] = parameters[i]
				setterParameters[i].Description += "set"
			}
			setterSignature := &api.FunctionSignature{
				FunctionName:          "set-" + name,
				Parameters:            setterParameters,
				Mutating:              true,
				Hermetic:              true,
				Idempotent:            true,
				Description:           "Set" + description,
				FunctionType:          api.FunctionTypePathVisitor,
				AttributeName:         attributeName,
				AffectedResourceTypes: resourceTypes,
			}
			if err := fh.RegisterFunction("set-"+name, &handler.FunctionRegistration{
				FunctionSignature: *setterSignature,
				Function: func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
					return genericFnSetVisitorDefaults(setterSignature, fArgs.FunctionContext, fArgs.ParsedData, fArgs.Arguments, resourceProvider, fArgs.Options)
				},
			}); err != nil {
				slog.Error("failed to register function", "error", err)
			}
		}

		// Vet function: takes only path parameters, validates defaults
		vetParameters := make([]api.FunctionParameter, len(parameters))
		for i := range vetParameters {
			vetParameters[i] = parameters[i]
			vetParameters[i].Description += "vet"
		}
		vetSignature := &api.FunctionSignature{
			FunctionName: "vet-" + name,
			Parameters:   vetParameters,
			OutputInfo: &api.FunctionOutput{
				ResultName:  "passed",
				Description: "True if all defaults are correctly set, false otherwise",
				OutputType:  api.OutputTypeValidationResult,
				Schema:      &api.ValidationResultListSchema,
			},
			Validating:            true,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Validate" + description,
			FunctionType:          api.FunctionTypePathVisitor,
			AttributeName:         attributeName,
			AffectedResourceTypes: resourceTypes,
		}
		if err := fh.RegisterFunction("vet-"+name, &handler.FunctionRegistration{
			FunctionSignature: *vetSignature,
			Function: func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
				return genericFnVetVisitorDefaults(vetSignature, fArgs.FunctionContext, fArgs.ParsedData, fArgs.Arguments, resourceProvider, fArgs.Options)
			},
		}); err != nil {
			slog.Error("failed to register function", "error", err)
		}
		return
	}

	// Standard mode: typed getter/setter with value parameter
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
		Schema:      &api.AttributeValueListSchema,
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
		setterFunction = func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
			return genericFnSetStringVisitor(setterSignature, fArgs.FunctionContext, fArgs.ParsedData, fArgs.Arguments, resourceProvider, upsert, fArgs.Options)
		}
		getterFunction = func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
			return genericFnGetStringVisitor(getterSignature, fArgs.FunctionContext, fArgs.ParsedData, fArgs.Arguments, resourceProvider, fArgs.Options)
		}
	case api.DataTypeInt:
		setterFunction = func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
			return genericFnSetIntVisitor(setterSignature, fArgs.FunctionContext, fArgs.ParsedData, fArgs.Arguments, resourceProvider, upsert, fArgs.Options)
		}
		getterFunction = func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
			return genericFnGetIntVisitor(getterSignature, fArgs.FunctionContext, fArgs.ParsedData, fArgs.Arguments, resourceProvider, fArgs.Options)
		}
	case api.DataTypeBool:
		setterFunction = func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
			return genericFnSetBoolVisitor(setterSignature, fArgs.FunctionContext, fArgs.ParsedData, fArgs.Arguments, resourceProvider, upsert, fArgs.Options)
		}
		getterFunction = func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
			return genericFnGetBoolVisitor(getterSignature, fArgs.FunctionContext, fArgs.ParsedData, fArgs.Arguments, resourceProvider, fArgs.Options)
		}
	default:
		// Not supported
		slog.Error("unsupported setter/getter data type", "dataType", string(dataType))
		return
	}
	if addSetter {
		if err := fh.RegisterFunction("set-"+name, &handler.FunctionRegistration{
			FunctionSignature: *setterSignature,
			Function:          setterFunction,
		}); err != nil {
			slog.Error("failed to register function", "error", err)
		}
	}
	if err := fh.RegisterFunction("get-"+name, &handler.FunctionRegistration{
		FunctionSignature: *getterSignature,
		Function:          getterFunction,
	}); err != nil {
		slog.Error("failed to register function", "error", err)
	}
}

func genericFnSetStringVisitor(signature *api.FunctionSignature, _ *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, resourceProvider yamlkit.ResourceProvider, upsert bool, options *api.FunctionOptions) (gaby.Container, any, error) {
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
	err := yamlkit.UpdateStringPaths(parsedData, resourceTypeToPaths, pathArgs, resourceProvider, valueToSet, upsert, options)
	return parsedData, nil, err
}

func genericFnGetStringVisitor(signature *api.FunctionSignature, _ *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, resourceProvider yamlkit.ResourceProvider, options *api.FunctionOptions) (gaby.Container, any, error) {
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
	values, err := yamlkit.GetStringPaths(parsedData, resourceTypeToPaths, pathArgs, resourceProvider, options)
	return parsedData, values, err
}

func genericFnSetIntVisitor(signature *api.FunctionSignature, _ *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, resourceProvider yamlkit.ResourceProvider, upsert bool, options *api.FunctionOptions) (gaby.Container, any, error) {
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
	err := yamlkit.UpdatePathsValue[int](parsedData, resourceTypeToPaths, pathArgs, resourceProvider, valueToSet, upsert, options)
	return parsedData, nil, err
}

func genericFnGetIntVisitor(signature *api.FunctionSignature, _ *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, resourceProvider yamlkit.ResourceProvider, options *api.FunctionOptions) (gaby.Container, any, error) {
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
	values, err := yamlkit.GetPaths[int](parsedData, resourceTypeToPaths, pathArgs, resourceProvider, options)
	return parsedData, values, err
}

func genericFnSetBoolVisitor(signature *api.FunctionSignature, _ *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, resourceProvider yamlkit.ResourceProvider, upsert bool, options *api.FunctionOptions) (gaby.Container, any, error) {
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
	err := yamlkit.UpdatePathsValue[bool](parsedData, resourceTypeToPaths, pathArgs, resourceProvider, valueToSet, upsert, options)
	return parsedData, nil, err
}

func genericFnGetBoolVisitor(signature *api.FunctionSignature, _ *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, resourceProvider yamlkit.ResourceProvider, options *api.FunctionOptions) (gaby.Container, any, error) {
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
	values, err := yamlkit.GetPaths[bool](parsedData, resourceTypeToPaths, pathArgs, resourceProvider, options)
	return parsedData, values, err
}

func genericFnGetVisitorAnyType(signature *api.FunctionSignature, _ *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, resourceProvider yamlkit.ResourceProvider, options *api.FunctionOptions) (gaby.Container, any, error) {
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
	values, err := yamlkit.GetPathsAnyType(parsedData, resourceTypeToPaths, pathArgs, resourceProvider, api.DataTypeNone, false, false, options)
	return parsedData, values, err
}

func genericFnSetVisitorDefaults(signature *api.FunctionSignature, _ *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, resourceProvider yamlkit.ResourceProvider, options *api.FunctionOptions) (gaby.Container, any, error) {
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
	err := yamlkit.UpdatePathsSetterArgument(parsedData, resourceTypeToPaths, pathArgs, resourceProvider, true, options)
	return parsedData, nil, err
}

func genericFnVetVisitorDefaults(signature *api.FunctionSignature, _ *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, resourceProvider yamlkit.ResourceProvider, options *api.FunctionOptions) (gaby.Container, any, error) {
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
	result, err := yamlkit.VetPathsSetterArgument(parsedData, resourceTypeToPaths, pathArgs, resourceProvider, options)
	return parsedData, result, err
}
