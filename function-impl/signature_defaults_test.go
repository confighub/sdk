// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package impl

import (
	"regexp"
	"slices"
	"strconv"
	"testing"

	"github.com/confighub/sdk/core/function/api"
)

// A parameter's DefaultValue is what the function does when the argument is omitted, so it has to
// be a value the parameter takes: of its data type, within its constraints, and not on a required
// parameter, which has no omitted case.
func TestBuiltInDefaultValuesAreValidArguments(t *testing.T) {
	defaults := 0
	for toolchain, functions := range NewStandardExecutor(nil, true).RegisteredFunctions() {
		for name, signature := range functions {
			for _, parameter := range signature.Parameters {
				value := parameter.DefaultValue
				if value == "" {
					continue
				}
				defaults++
				where := string(toolchain) + " " + name + " " + parameter.ParameterName
				if parameter.Required {
					t.Errorf("%s: a required parameter has a DefaultValue", where)
				}
				switch parameter.DataType {
				case api.DataTypeInt:
					n, err := strconv.Atoi(value)
					if err != nil {
						t.Errorf("%s: DefaultValue %q is not an int", where, value)
					} else if (parameter.Min != nil && n < *parameter.Min) || (parameter.Max != nil && n > *parameter.Max) {
						t.Errorf("%s: DefaultValue %d is out of range", where, n)
					}
				case api.DataTypeBool:
					if _, err := strconv.ParseBool(value); err != nil {
						t.Errorf("%s: DefaultValue %q is not a bool", where, value)
					}
				}
				if len(parameter.EnumValues) > 0 && !slices.Contains(parameter.EnumValues, value) {
					t.Errorf("%s: DefaultValue %q is not one of %v", where, value, parameter.EnumValues)
				}
				if parameter.Regexp != "" && !regexp.MustCompile(parameter.Regexp).MatchString(value) {
					t.Errorf("%s: DefaultValue %q does not match %s", where, value, parameter.Regexp)
				}
			}
		}
	}
	if defaults == 0 {
		t.Fatal("no built-in function declares a DefaultValue")
	}
}
