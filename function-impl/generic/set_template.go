// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package generic

import (
	"bytes"
	"fmt"
	"log/slog"
	"strings"
	"text/template"

	"github.com/confighub/sdk/core/configkit"
	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/function/handler"
	"github.com/confighub/sdk/core/third_party/gaby"
)

func registerSetTemplate(fh handler.FunctionRegistry, converter configkit.ConfigConverter, resourceProvider yamlkit.ResourceProvider) {
	if err := fh.RegisterFunction("set-template", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "set-template",
			Parameters: []api.FunctionParameter{
				{
					ParameterName: "expression",
					Required:      true,
					Description:   "Go text/template expression. FunctionContext fields are accessible at the top level (e.g. {{.UnitSlug}}); params passed via the `param` argument are accessible as {{.Params.<key>}}.",
					DataType:      api.DataTypeString,
					Example:       `{{.Params.repo}}:{{.Params.tag}}`,
				},
				{
					ParameterName:    "path",
					Required:         true,
					Description:      "Dot-separated path of the attribute to set. See https://docs.confighub.com/guide/functions/#configuration-path-syntax for more details regarding path syntax.",
					DataType:         api.DataTypeString,
					ValueConstraints: api.ValueConstraints{Regexp: api.PathRegexpString},
				},
				{
					ParameterName: "param",
					Required:      false,
					Description:   "Parameters passed to the template as key=value strings, accessible as {{.Params.<key>}} (values are strings).",
					DataType:      api.DataTypeString,
				},
			},
			VarArgs:               true,
			Mutating:              true,
			Validating:            false,
			Hermetic:              true,
			Idempotent:            true,
			Replayable:            true,
			Description:           "Renders a Go text/template expression and writes the result to the specified path of each resource matched by WhereResource. FunctionContext fields are accessible at the top level; vararg key=value params are accessible under .Params.",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
			return GenericFnSetTemplate(resourceProvider, fArgs.FunctionContext, fArgs.ParsedData, fArgs.Arguments, fArgs.Options)
		},
	}); err != nil {
		slog.Error("failed to register function", "error", err)
	}
}

// setTemplateScope is the value passed to Go template Execute. FunctionContext
// is embedded so its fields are accessible as {{.UnitSlug}}, {{.SpaceSlug}},
// etc.; vararg key=value params live under {{.Params.<key>}}.
type setTemplateScope struct {
	*api.FunctionContext
	Params map[string]string
}

// GenericFnSetTemplate implements set-template. It parses key=value params
// from args[2:], renders args[0] as a Go template with FunctionContext +
// Params in scope, and writes the rendered string to args[1] on every
// resource matched by WhereResource.
func GenericFnSetTemplate(resourceProvider yamlkit.ResourceProvider, functionContext *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, options *api.FunctionOptions) (gaby.Container, any, error) {
	expression, ok := args[0].Value.(string)
	if !ok {
		return parsedData, nil, fmt.Errorf("expression argument must be a string, got %T", args[0].Value)
	}
	unresolvedPath, ok := args[1].Value.(string)
	if !ok {
		return parsedData, nil, fmt.Errorf("path argument must be a string, got %T", args[1].Value)
	}

	params, err := parseSetTemplateParams(args, 2)
	if err != nil {
		return parsedData, nil, err
	}

	tmpl, err := template.New("set-template").Option("missingkey=error").Parse(expression)
	if err != nil {
		return parsedData, nil, fmt.Errorf("parse template: %w", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, setTemplateScope{FunctionContext: functionContext, Params: params}); err != nil {
		return parsedData, nil, fmt.Errorf("execute template: %w", err)
	}
	value := buf.String()

	resourceTypeToPaths := yamlkit.GetVisitorMapForPath(resourceProvider, api.ResourceTypeAny, api.UnresolvedPath(unresolvedPath))
	err = yamlkit.UpdateStringPaths(parsedData, resourceTypeToPaths, []any{}, resourceProvider, value, true, options)
	return parsedData, nil, err
}

// parseSetTemplateParams extracts key=value params from args[startIndex:].
func parseSetTemplateParams(args []api.FunctionArgument, startIndex int) (map[string]string, error) {
	params := make(map[string]string, len(args)-startIndex)
	for i := startIndex; i < len(args); i++ {
		s, ok := args[i].Value.(string)
		if !ok {
			return nil, fmt.Errorf("param argument %d must be a string, got %T", i, args[i].Value)
		}
		key, value, found := strings.Cut(s, "=")
		if !found {
			return nil, fmt.Errorf("param argument %d must be in key=value format, got %q", i, s)
		}
		params[key] = value
	}
	return params, nil
}
