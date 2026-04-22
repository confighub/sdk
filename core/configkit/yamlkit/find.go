// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package yamlkit

import (
	"sort"
	"strconv"
	"strings"

	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/third_party/gaby"
)

func newAttributeValue(path api.ResolvedPath, resourceInfo *api.ResourceInfo, value any) api.AttributeValue {
	// TODO: attributeName, dataType, Info.GetterInvocation, Info.SetterInvocations, Comment
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

// FindYAMLPathsByValue searches for all paths that match a specified value in a YAML structure
// and returns an api.AttributeValueList.
func FindYAMLPathsByValue(parsedData gaby.Container, resourceProvider ResourceProvider, searchValue any, options *api.FunctionOptions) api.AttributeValueList {
	var paths api.AttributeValueList

	searchStringValue, searchValueIsString := searchValue.(string)

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
				// TODO: factor this out into a function
				// Check if the value of the current key matches the search value
				if child.Data() == searchValue {
					attributeValue := AttributeValueForPath(resourceProvider, api.ResolvedPath(currentPath), resourceInfo, searchValue)
					paths = append(paths, attributeValue)
					// Skip further traversal since the match is found
					continue
				} else if searchValueIsString {
					stringVal, isString := child.Data().(string)
					if isString && strings.Contains(stringVal, searchStringValue) {
						attributeValue := AttributeValueForPath(resourceProvider, api.ResolvedPath(currentPath), resourceInfo, stringVal)
						paths = append(paths, attributeValue)
						// Skip further traversal since the match is found
						continue
					}
				}
				// Recursively traverse the YAML structure
				traverse(currentPath, child, resourceInfo)
			}
		} else if arrayChildren := doc.Children(); arrayChildren != nil {
			// NOTE: We'll also land here in the case of an empty map.

			// If the doc is an array, traverse its elements
			for index, child := range arrayChildren {
				currentPath := path + "." + strconv.Itoa(index)
				// Check if the value of the current array element matches the search value
				if child.Data() == searchValue {
					attributeValue := AttributeValueForPath(resourceProvider, api.ResolvedPath(currentPath), resourceInfo, searchValue)
					paths = append(paths, attributeValue)
					// Skip further traversal since the match is found
					continue
				} else if searchValueIsString {
					stringVal, isString := child.Data().(string)
					if isString && strings.Contains(stringVal, searchStringValue) {
						attributeValue := AttributeValueForPath(resourceProvider, api.ResolvedPath(currentPath), resourceInfo, stringVal)
						paths = append(paths, attributeValue)
						// Skip further traversal since the match is found
						continue
					}
				}
				// Recursively traverse the YAML structure
				traverse(currentPath, child, resourceInfo)
			}
		} else {
			// If the doc is neither a map nor an array, it's a value; compare it
			if path != "" {
				if doc.Data() == searchValue {
					attributeValue := AttributeValueForPath(resourceProvider, api.ResolvedPath(path), resourceInfo, searchValue)
					paths = append(paths, attributeValue)
				} else if searchValueIsString {
					stringVal, isString := doc.Data().(string)
					if isString && strings.Contains(stringVal, searchStringValue) {
						attributeValue := AttributeValueForPath(resourceProvider, api.ResolvedPath(path), resourceInfo, stringVal)
						paths = append(paths, attributeValue)
					}
				}
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
