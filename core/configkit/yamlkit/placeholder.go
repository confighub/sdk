// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package yamlkit

import (
	"regexp"
	"strings"

	"github.com/confighub/sdk/core/function/api"
)

// PlaceHolderBlockApply We will need placeholders for different data types and that fit with different validation rules
// The string value is all lowercase to comply with DNS label requirements.
const (
	PlaceHolderBlockApplyString = "confighubplaceholder"
	PlaceHolderBlockApplyInt    = 999999999
)

// placeholderStringRegexp matches the placeholder string PlaceHolderBlockApplyString
// and "named placeholders" — the placeholder string immediately followed by
// a name of letters, digits and underscores, with no punctuation, whitespace, or
// other characters in between. For example, it matches "confighubplaceholdersubdomain"
// in "confighubplaceholdersubdomain.test.example.com", "confighubplaceholderapp_env"
// in "confighubplaceholderapp_env", and "confighubplaceholder" in
// "confighubplaceholder-http".
var placeholderStringRegexp = regexp.MustCompile(PlaceHolderBlockApplyString + placeholderNamePattern)

// placeholderNamePattern is what may follow the placeholder string as its name. A dash
// ends the name, so that "confighubplaceholder-http" is the bare placeholder followed by
// "-http"; an underscore stands in for the dashes and dots in a resource's name.
const placeholderNamePattern = "[a-zA-Z0-9_]*"

var wholeNamedPlaceholderRegexp = regexp.MustCompile("^" + PlaceHolderBlockApplyString + "(" + placeholderNamePattern + ")$")

// PlaceholderName returns the name of the named placeholder that s consists of, and
// whether s is one. A bare placeholder has no name, and neither does a value with a
// placeholder embedded in other text: a name identifies what the whole value is to be
// filled from.
func PlaceholderName(s string) (string, bool) {
	m := wholeNamedPlaceholderRegexp.FindStringSubmatch(s)
	if m == nil || m[1] == "" {
		return "", false
	}
	return m[1], true
}

// PlaceholderNameFor returns the placeholder name that stands for a resource name: the
// name with its dashes and dots, which can't appear in a placeholder name, replaced by
// underscores. "app-env" is named by "confighubplaceholderapp_env".
func PlaceholderNameFor(resourceName string) string {
	return strings.NewReplacer("-", "_", ".", "_").Replace(resourceName)
}

// PreferPlaceholderName has a needed value that was a named placeholder, such as
// confighubplaceholderapp_env, prefer a provided value that offers its name, app_env, as
// api.PropertyKeyPlaceholderName. original is what the needed path held when it was found
// to be needed, which the stored NeededPath keeps after a resolve writes over it.
func PreferPlaceholderName(attr *api.AttributeValue, original any) {
	s, ok := original.(string)
	if !ok {
		return
	}
	name, named := PlaceholderName(s)
	if !named {
		return
	}
	if attr.Details == nil {
		attr.Details = &api.AttributeDetails{}
	}
	if attr.Details.NeededPreferred == nil {
		attr.Details.NeededPreferred = map[string]string{}
	}
	attr.Details.NeededPreferred[api.PropertyKeyPlaceholderName] = name
}

// NamedPlaceholderFor returns the named placeholder that stands for a resource name.
func NamedPlaceholderFor(resourceName string) string {
	return PlaceHolderBlockApplyString + PlaceholderNameFor(resourceName)
}

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
