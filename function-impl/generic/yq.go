// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package generic

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/confighub/sdk/configkit/yqkit"
	"github.com/confighub/sdk/core/configkit"
	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/function/handler"
	"github.com/confighub/sdk/core/third_party/gaby"
	"sigs.k8s.io/yaml"
)

// buildYQParamsPrefix parses key=value params from args[startIndex:] and returns
// a yq expression prefix that binds them to $params, e.g. `{"foo":"abc"} as $params | `.
// Returns the empty string when no params are provided.
func buildYQParamsPrefix(args []api.FunctionArgument, startIndex int) (string, error) {
	if startIndex >= len(args) {
		return "", nil
	}
	params := make(map[string]string, len(args)-startIndex)
	for i := startIndex; i < len(args); i++ {
		s, ok := args[i].Value.(string)
		if !ok {
			return "", fmt.Errorf("param argument %d must be a string, got %T", i, args[i].Value)
		}
		key, value, found := strings.Cut(s, "=")
		if !found {
			return "", fmt.Errorf("param argument %d must be in key=value format, got %q", i, s)
		}
		params[key] = value
	}
	paramsJSON, err := json.Marshal(params)
	if err != nil {
		return "", fmt.Errorf("failed to encode params: %w", err)
	}
	return string(paramsJSON) + " as $params | ", nil
}

func registerYQ(fh handler.FunctionRegistry, converter configkit.ConfigConverter, resourceProvider yamlkit.ResourceProvider) {
	if err := fh.RegisterFunction("get-yq", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "get-yq",
			Parameters: []api.FunctionParameter{
				{
					ParameterName: "yq-expression",
					Required:      true,
					Description:   "yq expression. Use `select() | ...` to get values from a subset of the documents. Params passed via the `param` argument are accessible as `$params.<key>` (e.g. `$params.foo`).",
					DataType:      api.DataTypeString,
					Example:       ".spec.replicas",
				},
				{
					ParameterName: "param",
					Required:      false,
					Description:   "Parameters passed to the yq expression as key=value strings, accessible as `$params.<key>` (values are strings; use `| tonumber` or `| tobool` to coerce).",
					DataType:      api.DataTypeString,
				},
			},
			VarArgs: true,
			OutputInfo: &api.FunctionOutput{
				ResultName:  "yq output",
				Description: "Output from yq",
				OutputType:  api.OutputTypeYAML,
				Schema:      &api.YAMLPayloadSchema,
			},
			Mutating:              false,
			Validating:            false,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Returns the result of running yq with the specified expression on the configuration data filtered by WhereResource.",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
			prefix, err := buildYQParamsPrefix(fArgs.Arguments, 1)
			if err != nil {
				return fArgs.ParsedData, nil, err
			}
			expression := prefix + fArgs.Arguments[0].Value.(string)
			return genericFnYQQuery(resourceProvider, fArgs.FunctionContext, fArgs.ParsedData, expression, fArgs.Options)
		},
	}); err != nil {
		slog.Error("failed to register function", "error", err)
	}
	// TODO: Deprecated in favor of get-yq. Remove this.
	if err := fh.RegisterFunction("yq", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "yq",
			Parameters: []api.FunctionParameter{
				{
					ParameterName: "yq-expression",
					Required:      true,
					Description:   "yq expression. Use `select() | ...` to get values from a subset of the documents. Params passed via the `param` argument are accessible as `$params.<key>` (e.g. `$params.foo`).",
					DataType:      api.DataTypeString,
					Example:       ".spec.replicas",
				},
				{
					ParameterName: "param",
					Required:      false,
					Description:   "Parameters passed to the yq expression as key=value strings, accessible as `$params.<key>` (values are strings; use `| tonumber` or `| tobool` to coerce).",
					DataType:      api.DataTypeString,
				},
			},
			VarArgs: true,
			OutputInfo: &api.FunctionOutput{
				ResultName:  "yq output",
				Description: "Output from yq",
				OutputType:  api.OutputTypeYAML,
				Schema:      &api.YAMLPayloadSchema,
			},
			Mutating:              false,
			Validating:            false,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "[Deprecated; use get-yq instead] Returns the result of running yq with the specified expression on the configuration data filtered by WhereResource.",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
			prefix, err := buildYQParamsPrefix(fArgs.Arguments, 1)
			if err != nil {
				return fArgs.ParsedData, nil, err
			}
			expression := prefix + fArgs.Arguments[0].Value.(string)
			return genericFnYQQuery(resourceProvider, fArgs.FunctionContext, fArgs.ParsedData, expression, fArgs.Options)
		},
	}); err != nil {
		slog.Error("failed to register function", "error", err)
	}
}

func registerYQI(fh handler.FunctionRegistry, converter configkit.ConfigConverter, resourceProvider yamlkit.ResourceProvider) {
	if err := fh.RegisterFunction("set-yq", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "set-yq",
			Parameters: []api.FunctionParameter{
				{
					ParameterName: "yq-expression",
					Required:      true,
					Description:   "yq expression. Use `with(select(); ...)` to modify values in a subset of the documents. Params passed via the `param` argument are accessible as `$params.<key>` (e.g. `$params.foo`).",
					DataType:      api.DataTypeString,
					Example:       ".spec.replicas = 7",
				},
				{
					ParameterName: "param",
					Required:      false,
					Description:   "Parameters passed to the yq expression as key=value strings, accessible as `$params.<key>` (values are strings; use `| tonumber` or `| tobool` to coerce).",
					DataType:      api.DataTypeString,
				},
			},
			VarArgs:               true,
			Mutating:              true,
			Validating:            false,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "The configuration data is updated with the result of running yq -i with the specified expression on the configuration data filtered by WhereResource.",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
			prefix, err := buildYQParamsPrefix(fArgs.Arguments, 1)
			if err != nil {
				return fArgs.ParsedData, nil, err
			}
			expression := prefix + fArgs.Arguments[0].Value.(string)
			return genericFnYQMutating(resourceProvider, fArgs.ParsedData, expression, fArgs.Options)
		},
	}); err != nil {
		slog.Error("failed to register function", "error", err)
	}
	// TODO: Deprecated in favor of set-yq. Remove this.
	if err := fh.RegisterFunction("yq-i", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "yq-i",
			Parameters: []api.FunctionParameter{
				{
					ParameterName: "yq-expression",
					Required:      true,
					Description:   "yq expression. Use `with(select(); ...)` to modify values in a subset of the documents. Params passed via the `param` argument are accessible as `$params.<key>` (e.g. `$params.foo`).",
					DataType:      api.DataTypeString,
					Example:       ".spec.replicas = 7",
				},
				{
					ParameterName: "param",
					Required:      false,
					Description:   "Parameters passed to the yq expression as key=value strings, accessible as `$params.<key>` (values are strings; use `| tonumber` or `| tobool` to coerce).",
					DataType:      api.DataTypeString,
				},
			},
			VarArgs:               true,
			Mutating:              true,
			Validating:            false,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "[Deprecated; use set-yq instead] The configuration data is updated with the result of running yq -i with the specified expression on the configuration data filtered by WhereResource.",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
			prefix, err := buildYQParamsPrefix(fArgs.Arguments, 1)
			if err != nil {
				return fArgs.ParsedData, nil, err
			}
			expression := prefix + fArgs.Arguments[0].Value.(string)
			return genericFnYQMutating(resourceProvider, fArgs.ParsedData, expression, fArgs.Options)
		},
	}); err != nil {
		slog.Error("failed to register function", "error", err)
	}
}

type resourceContext struct {
	api.FunctionContext
	api.ResourceInfo
}

// genericFnYQQuery runs the yq expression on each filtered resource and assembles
// the output YAML from all results.
func genericFnYQQuery(resourceProvider yamlkit.ResourceProvider, functionContext *api.FunctionContext, parsedData gaby.Container, expression string, options *api.FunctionOptions) (gaby.Container, any, error) {
	var outputDocs gaby.Container
	_, err := yamlkit.VisitResourcesFiltered(parsedData, nil, resourceProvider, options,
		func(doc *gaby.YamlDoc, output any, _ int, ri *api.ResourceInfo) (any, []error) {
			singleDoc := gaby.Container{doc}
			data := singleDoc.String()
			// Append ConfigHub metadata and resource metadata for reference in yq output
			metadata := map[string]*resourceContext{}
			metadata["ConfigHub"] = &resourceContext{
				FunctionContext: *functionContext,
				ResourceInfo:    *ri,
			}
			metadataBytes, err := yaml.Marshal(metadata)
			if err == nil {
				data += string(metadataBytes)
			}
			result, err := yqkit.EvalYQExpression(expression, data)
			if err != nil {
				return output, []error{errors.Wrap(err, "yq expression evaluation failed")}
			}
			parsed, err := gaby.ParseAll([]byte(result))
			if err != nil {
				return output, []error{errors.Wrap(err, "failed to parse output from yq")}
			}
			outputDocs = append(outputDocs, parsed...)
			return output, nil
		})
	if err != nil {
		return parsedData, nil, err
	}
	wrappedOutput := api.YAMLPayload{Payload: outputDocs.String()}
	return parsedData, wrappedOutput, nil
}

// genericFnYQMutating runs the yq expression on each filtered resource in place,
// replacing matched documents in parsedData with the yq output while leaving
// unmatched documents unchanged.
func genericFnYQMutating(resourceProvider yamlkit.ResourceProvider, parsedData gaby.Container, expression string, options *api.FunctionOptions) (gaby.Container, any, error) {
	// Collect replacements keyed by index in parsedData. Each replacement may
	// produce zero, one, or multiple documents.
	replacements := map[int]gaby.Container{}

	_, err := yamlkit.VisitResourcesFiltered(parsedData, nil, resourceProvider, options,
		func(doc *gaby.YamlDoc, output any, index int, _ *api.ResourceInfo) (any, []error) {
			singleDoc := gaby.Container{doc}
			yqOutput, err := yqkit.EvalYQExpression(expression, singleDoc.String())
			if err != nil {
				return output, []error{errors.Wrap(err, "yq expression evaluation failed")}
			}
			parsed, err := gaby.ParseAll([]byte(yqOutput))
			if err != nil {
				return output, []error{errors.Wrap(err, "failed to parse output from yq")}
			}
			replacements[index] = parsed
			return output, nil
		})
	if err != nil {
		return parsedData, nil, err
	}

	// Assemble result: for each original doc, use the replacement if one was
	// recorded, otherwise keep the original.
	var result gaby.Container
	for i, doc := range parsedData {
		if replacement, ok := replacements[i]; ok {
			result = append(result, replacement...)
		} else {
			result = append(result, doc)
		}
	}
	return result, nil, nil
}
