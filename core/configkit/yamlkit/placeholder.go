// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package yamlkit

import (
	"regexp"
	"strings"
)

// PlaceHolderBlockApply We will need placeholders for different data types and that fit with different validation rules
// The string value is all lowercase to comply with DNS label requirements.
const (
	PlaceHolderBlockApplyString = "confighubplaceholder"
	PlaceHolderBlockApplyInt    = 999999999
)

// placeholderStringRegexp matches the placeholder string PlaceHolderBlockApplyString
// and "named placeholders" — the placeholder string immediately followed by
// additional alphabetic characters with no punctuation, whitespace, or other
// characters in between. For example, it matches "confighubplaceholdersubdomain"
// in "confighubplaceholdersubdomain.test.example.com" and "confighubplaceholder"
// in "confighubplaceholder-http".
var placeholderStringRegexp = regexp.MustCompile(PlaceHolderBlockApplyString + "[a-zA-Z]*")

// ReplaceStringPlaceholder replaces all occurrences of the placeholder string
// PlaceHolderBlockApplyString in s with replacement, including named placeholders
// that consist of the placeholder string followed by additional alphabetic
// characters. Surrounding text is preserved, so "confighubplaceholder-http"
// becomes replacement + "-http" and "confighubplaceholdersubdomain.test.example.com"
// becomes replacement + ".test.example.com".
func ReplaceStringPlaceholder(s, replacement string) string {
	return placeholderStringRegexp.ReplaceAllString(s, replacement)
}

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
