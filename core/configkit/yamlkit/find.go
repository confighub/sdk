// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package yamlkit

import (
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/third_party/gaby"
)

// ValueMatcher decides whether a scalar value found while traversing a YAML
// structure should be collected by FindYAMLPathsByValue.
type ValueMatcher interface {
	// Matches reports whether the given scalar value (string, int, float64,
	// bool, etc.) matches.
	Matches(value any) bool
}

// ExactValueMatcher matches values that are equal to Value. It is used for
// non-string values such as the integer placeholder, where substring or regular
// expression matching does not apply.
type ExactValueMatcher struct {
	Value any
}

func (m ExactValueMatcher) Matches(value any) bool {
	return value == m.Value
}

// StringContainsMatcher matches string values that contain Substring.
type StringContainsMatcher struct {
	Substring string
}

func (m StringContainsMatcher) Matches(value any) bool {
	s, ok := value.(string)
	return ok && strings.Contains(s, m.Substring)
}

// RegexpMatcher matches string values for which Regexp finds a match, providing
// sed-like regular expression matching.
type RegexpMatcher struct {
	Regexp *regexp.Regexp
}

func (m RegexpMatcher) Matches(value any) bool {
	s, ok := value.(string)
	return ok && m.Regexp.MatchString(s)
}

func newAttributeValue(path api.ResolvedPath, resourceInfo *api.ResourceInfo, value any) api.AttributeValue {
	// TODO: attributeName, dataType, Comment
	var attributeValue api.AttributeValue
	attributeValue.ResourceInfo = *resourceInfo
	attributeValue.Path = path
	attributeValue.AttributeName = api.AttributeNameNone
	attributeValue.Value = value
	switch value.(type) {
	case string:
		attributeValue.DataType = api.DataTypeString
	case int:
		attributeValue.DataType = api.DataTypeInt
	case float64:
		// Ints are represented as "numbers", which parse as float64s
		attributeValue.DataType = api.DataTypeInt
	case bool:
		attributeValue.DataType = api.DataTypeBool
	default:
		// TODO: This may not be the best choice
		attributeValue.DataType = api.DataTypeJSON
	}
	return attributeValue
}

func AttributeValueForPath(resourceProvider ResourceProvider, path api.ResolvedPath, resourceInfo *api.ResourceInfo, value any) api.AttributeValue {
	attributeValue := newAttributeValue(path, resourceInfo, value)

	resourceType := api.ResourceType("*")
	if resourceInfo != nil {
		resourceType = resourceInfo.ResourceType
	}
	visitorInfo := GetPathVisitorInfo(resourceProvider, resourceType, api.UnresolvedPath(path))
	if visitorInfo != nil {
		attributeValue.AttributeInfo.AttributeName = visitorInfo.AttributeName
	}

	return attributeValue
}

// FindYAMLPathsByValue searches for all paths whose scalar value is matched by
// matcher in a YAML structure and returns an api.AttributeValueList. The matched
// value stored in each AttributeValue is the actual value found at the path, so
// callers (e.g. search-replace) can transform it.
func FindYAMLPathsByValue(parsedData gaby.Container, resourceProvider ResourceProvider, matcher ValueMatcher, options *api.FunctionOptions) api.AttributeValueList {
	var paths api.AttributeValueList

	// tryMatch appends an AttributeValue for the value at path if matcher matches
	// it, and reports whether it matched so the caller can skip further traversal.
	tryMatch := func(path string, doc *gaby.YamlDoc, resourceInfo *api.ResourceInfo) bool {
		value := doc.Data()
		if !matcher.Matches(value) {
			return false
		}
		attributeValue := AttributeValueForPath(resourceProvider, api.ResolvedPath(path), resourceInfo, value)
		paths = append(paths, attributeValue)
		return true
	}

	// Recursive function to traverse YAML structure
	// TODO: use a worklist instead of recursion so that we can't blow our stack
	var traverse func(path string, doc *gaby.YamlDoc, resourceInfo *api.ResourceInfo)
	traverse = func(path string, doc *gaby.YamlDoc, resourceInfo *api.ResourceInfo) {
		children := doc.ChildrenMap()
		if len(children) > 0 {
			// If the container is a map, traverse its children
			for key, child := range children {
				var currentPath string
				// The key needs to be escaped so that the path can be parsed when passed back into functions
				escapedKey := EscapeDotsInPathSegment(key)
				if path != "" {
					currentPath = path + "." + escapedKey
				} else {
					currentPath = escapedKey
				}
				// Check if the value of the current key matches; if so, skip
				// further traversal since the match is found.
				if tryMatch(currentPath, child, resourceInfo) {
					continue
				}
				// Recursively traverse the YAML structure
				traverse(currentPath, child, resourceInfo)
			}
		} else if arrayChildren := doc.Children(); arrayChildren != nil {
			// NOTE: We'll also land here in the case of an empty map.

			// If the doc is an array, traverse its elements
			for index, child := range arrayChildren {
				currentPath := path + "." + strconv.Itoa(index)
				// Check if the value of the current array element matches; if so,
				// skip further traversal since the match is found.
				if tryMatch(currentPath, child, resourceInfo) {
					continue
				}
				// Recursively traverse the YAML structure
				traverse(currentPath, child, resourceInfo)
			}
		} else {
			// If the doc is neither a map nor an array, it's a value; compare it
			if path != "" {
				tryMatch(path, doc, resourceInfo)
			}
		}
	}

	visitor := func(doc *gaby.YamlDoc, _ any, _ int, resourceInfo *api.ResourceInfo) (any, []error) {
		// Start traversal from the root
		traverse("", doc, resourceInfo)
		return nil, []error{}
	}
	VisitResourcesFiltered(parsedData, nil, resourceProvider, options, visitor)

	// TODO: Revisit. Did this for predictable order.
	sort.Slice(paths, attributeValueCompareFunction(paths))

	return paths
}
