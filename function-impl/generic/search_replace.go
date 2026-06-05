// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package generic

import (
	"log/slog"
	"regexp"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/confighub/sdk/core/configkit"
	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/function/handler"
	"github.com/confighub/sdk/core/third_party/gaby"
)

func registerSearchReplace(fh handler.FunctionRegistry, converter configkit.ConfigConverter, resourceProvider yamlkit.ResourceProvider) {
	if err := fh.RegisterFunction("search-replace", &handler.FunctionRegistration{
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
					Description:   "String value to use as the replacement for search-value. When regexp is true, $1, $name, ${name} references expand to submatches, as with sed",
					DataType:      api.DataTypeString,
				},
				{
					ParameterName: "regexp",
					Required:      false,
					Description:   "If true, interpret search-value as an RE2 regular expression and expand submatch references in replace-value, conceptually similar to sed. Defaults to false (literal substring replacement).",
					DataType:      api.DataTypeBool,
					Example:       "false",
				},
			},
			Mutating:              true,
			Validating:            false,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Replace all instances of the search-value in all strings of all resource types with replace-value. Set regexp to true to match search-value as a regular expression and expand submatch references in replace-value, similar to sed.",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
			return genericFnSearchReplace(resourceProvider, fArgs.FunctionContext, fArgs.ParsedData, fArgs.Arguments, fArgs.Options)
		},
	}); err != nil {
		slog.Error("failed to register function", "error", err)
	}
}

func genericFnSearchReplace(resourceProvider yamlkit.ResourceProvider, functionContext *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, options *api.FunctionOptions) (gaby.Container, any, error) {
	searchValue := args[0].Value.(string)
	replaceValue := args[1].Value.(string)

	// The optional regexp argument switches from literal substring matching to
	// RE2 regular expression matching with sed-like submatch expansion.
	useRegexp := false
	if len(args) > 2 {
		if b, ok := args[2].Value.(bool); ok {
			useRegexp = b
		}
	}

	var matcher yamlkit.ValueMatcher
	var re *regexp.Regexp
	if useRegexp {
		var err error
		re, err = regexp.Compile(searchValue)
		if err != nil {
			return parsedData, nil, errors.Wrap(err, "invalid regular expression in search-value")
		}
		matcher = yamlkit.RegexpMatcher{Regexp: re}
	} else {
		matcher = yamlkit.StringContainsMatcher{Substring: searchValue}
	}

	attributeList := yamlkit.FindYAMLPathsByValue(parsedData, resourceProvider, matcher, options)
	for i := range attributeList {
		existingValue := attributeList[i].Value.(string)
		if useRegexp {
			attributeList[i].Value = re.ReplaceAllString(existingValue, replaceValue)
		} else {
			attributeList[i].Value = strings.ReplaceAll(existingValue, searchValue, replaceValue)
		}
	}

	return genericSetAttributesFromList(resourceProvider, functionContext, parsedData, attributeList, options)
}
