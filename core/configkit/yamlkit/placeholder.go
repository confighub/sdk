// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package yamlkit

import "strings"

// PlaceHolderBlockApply We will need placeholders for different data types and that fit with different validation rules
// The string value is all lowercase to comply with DNS label requirements.
const (
	PlaceHolderBlockApplyString = "confighubplaceholder"
	PlaceHolderBlockApplyInt    = 999999999
)

func IsEmptyOrPlaceHolder(s string) bool {
	return s == "" || IsStringPlaceHolderValue(s)
}

func IsStringPlaceHolderValue(v string) bool {
	return strings.Contains(v, PlaceHolderBlockApplyString)
}

func IsIntPlaceHolderValue(v int) bool {
	return v == PlaceHolderBlockApplyInt || v == -PlaceHolderBlockApplyInt
}

// IsPlaceholderValue returns true if the value is a placeholder that should be replaced with a default.
func IsPlaceholderValue(value any) bool {
	// Treat no value as a placeholder value
	if value == nil {
		return true
	}
	switch v := value.(type) {
	case string:
		return IsStringPlaceHolderValue(v)
	case int:
		return IsIntPlaceHolderValue(v)
	default:
		return false
	}
}
