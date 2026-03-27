// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package generic

import (
	"encoding/json"
	"regexp"

	"github.com/cockroachdb/errors"
	"log/slog"
	"github.com/swaggest/jsonschema-go"

	"github.com/confighub/sdk/core/configkit"
	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/function/handler"
	"github.com/confighub/sdk/core/third_party/gaby"
)

func registerVetValues(fh handler.FunctionRegistry, converter configkit.ConfigConverter, resourceProvider yamlkit.ResourceProvider) {
	reflector := jsonschema.Reflector{}
	validationResultSchema, err := reflector.Reflect(api.ValidationResult{})
	if err != nil {
		slog.Error("couldn't get schema for api.ValidationResult")
	}
	valueFilterSchema, err := reflector.Reflect(api.ValueFilter{})
	if err != nil {
		slog.Error("couldn't get schema for api.ValueFilter")
	}
	fh.RegisterFunction("vet-values", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "vet-values",
			Parameters: []api.FunctionParameter{
				{
					ParameterName: "attribute-name",
					Required:      true,
					Description:   "Name of the attribute whose registered paths to check",
					DataType:      api.DataTypeString,
				},
				{
					ParameterName:    "value-filter",
					Required:         true,
					Description:      "JSON object specifying allow/deny rules for filtering values",
					DataType:         api.DataTypeValueFilter,
					ValueConstraints: api.ValueConstraints{Schema: &valueFilterSchema},
				},
			},
			OutputInfo: &api.FunctionOutput{
				ResultName:  "passed",
				Description: "True if all values pass the filter, false otherwise",
				OutputType:  api.OutputTypeValidationResult,
				Schema:      &validationResultSchema,
			},
			Mutating:              false,
			Validating:            true,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Validates that all values at the paths registered for the specified attribute pass the specified allow/deny filter. Supports string, int, and bool values.",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
			return genericFnVetValues(resourceProvider, fArgs.ParsedData, fArgs.Arguments)
		},
	})
}

func genericFnVetValues(resourceProvider yamlkit.ResourceProvider, parsedData gaby.Container, args []api.FunctionArgument) (gaby.Container, any, error) {
	attributeName := api.AttributeName(args[0].Value.(string))
	filterString := args[1].Value.(string)
	var filter api.ValueFilter
	if err := json.Unmarshal([]byte(filterString), &filter); err != nil {
		return parsedData, nil, errors.Wrap(err, "failed to parse value-filter argument")
	}

	result, err := GenericVetValues(resourceProvider, parsedData, attributeName, &filter)
	return parsedData, result, err
}

// GenericVetValues validates all values at paths registered for the given attribute name
// against the provided ValueFilter. It is exported so that other functions (e.g. vet-images)
// can call it.
func GenericVetValues(resourceProvider yamlkit.ResourceProvider, parsedData gaby.Container, attributeName api.AttributeName, filter *api.ValueFilter) (api.ValidationResult, error) {
	if err := validateFilter(filter); err != nil {
		return api.ValidationResult{}, err
	}

	resourceTypeToPaths := yamlkit.GetPathRegistryForAttributeName(resourceProvider, attributeName)
	values, err := yamlkit.GetPathsAnyType(parsedData, resourceTypeToPaths, []any{"*"}, resourceProvider, api.DataTypeNone, false, false, nil)
	if err != nil {
		return api.ValidationResult{}, errors.Wrap(err, "failed to get values for attribute")
	}

	var allowRegexp, denyRegexp *regexp.Regexp
	if filter.AllowRegexp != "" {
		allowRegexp, err = regexp.Compile(filter.AllowRegexp)
		if err != nil {
			return api.ValidationResult{}, errors.Wrap(err, "invalid AllowRegexp")
		}
	}
	if filter.DenyRegexp != "" {
		denyRegexp, err = regexp.Compile(filter.DenyRegexp)
		if err != nil {
			return api.ValidationResult{}, errors.Wrap(err, "invalid DenyRegexp")
		}
	}

	var failedAttributes api.AttributeValueList
	for _, attr := range values {
		if failed := checkValue(attr.Value, filter, allowRegexp, denyRegexp); failed {
			failedAttributes = append(failedAttributes, attr)
		}
	}

	result := api.ValidationResult{
		Passed:           len(failedAttributes) == 0,
		FailedAttributes: failedAttributes,
	}
	return result, nil
}

func validateFilter(filter *api.ValueFilter) error {
	if len(filter.AllowStrings) > 0 && filter.AllowRegexp != "" {
		return errors.New("AllowStrings and AllowRegexp are mutually exclusive")
	}
	if len(filter.DenyStrings) > 0 && filter.DenyRegexp != "" {
		return errors.New("DenyStrings and DenyRegexp are mutually exclusive")
	}
	return nil
}

func checkValue(value any, filter *api.ValueFilter, allowRegexp, denyRegexp *regexp.Regexp) bool {
	switch v := value.(type) {
	case string:
		return checkStringValue(v, filter, allowRegexp, denyRegexp)
	case int:
		return checkIntValue(v, filter)
	case bool:
		return checkBoolValue(v, filter)
	default:
		// Unknown type; skip
		return false
	}
}

func checkStringValue(v string, filter *api.ValueFilter, allowRegexp, denyRegexp *regexp.Regexp) bool {
	// Check deny first (deny overrides allow)
	if filter.DenyStrings[v] {
		return true
	}
	if denyRegexp != nil && denyRegexp.MatchString(v) {
		return true
	}
	// Check allow
	if len(filter.AllowStrings) > 0 && !filter.AllowStrings[v] {
		return true
	}
	if allowRegexp != nil && !allowRegexp.MatchString(v) {
		return true
	}
	return false
}

func checkIntValue(v int, filter *api.ValueFilter) bool {
	if filter.Min != nil && v < *filter.Min {
		return true
	}
	if filter.Max != nil && v > *filter.Max {
		return true
	}
	return false
}

func checkBoolValue(v bool, filter *api.ValueFilter) bool {
	if filter.AllowBool != nil && v != *filter.AllowBool {
		return true
	}
	return false
}

