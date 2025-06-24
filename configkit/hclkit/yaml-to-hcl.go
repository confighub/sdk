// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package hclkit

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/confighub/sdk/configkit/yamlkit"
	"github.com/confighub/sdk/function/api"
	"github.com/confighub/sdk/third_party/gaby"
	"gopkg.in/yaml.v3"
)

// YAMLToHCL converts a list of YAML documents to OpenTofu HCL blocks
func YAMLToHCL(yamlData []byte) ([]byte, error) {
	parsedData, err := gaby.ParseAll(yamlData)
	if err != nil {
		return nil, fmt.Errorf("failed to parse YAML document list: %w", err)
	}
	_, categoryTypeMap, err := HclResourceProvider.ResourceAndCategoryTypeMaps(parsedData)

	var hclBlocks []string
	for _, doc := range parsedData {
		docBytes := doc.Bytes()

		var data interface{}
		if err := yaml.Unmarshal(docBytes, &data); err != nil {
			return nil, fmt.Errorf("failed to parse YAML document: %w", err)
		}

		hclBlock, _, err := convertToHCL(categoryTypeMap, "", data, 0)
		if err != nil {
			return nil, fmt.Errorf("failed to convert to HCL: %w", err)
		}

		if strings.TrimSpace(hclBlock) != "" {
			hclBlocks = append(hclBlocks, hclBlock)
		}
	}

	result := strings.Join(hclBlocks, "\n\n") + "\n"
	return []byte(result), nil
}

func isReference(categoryTypeMap yamlkit.ResourceCategoryTypeToNamesMap, s string) bool {
	// fmt.Printf("string %s\n", s)
	segments := strings.Split(s, ".")
	if len(segments) > 1 {
		var category api.ResourceCategory
		var typeIndex int
		if len(segments) > 2 && segments[0] == "data" {
			category = api.ResourceCategoryDyanmicData
			typeIndex = 1
		} else {
			category = api.ResourceCategoryResource
			typeIndex = 0
		}
		categoryType := api.ResourceCategoryType{category, api.ResourceType(segments[typeIndex])}
		name := segments[typeIndex+1]
		// fmt.Printf("type %s, name %s\n", string(categoryType.ResourceType), name)
		names, found := categoryTypeMap[categoryType]
		if !found {
			return false
		}
		for _, foundName := range names {
			if name == string(foundName) {
				return true
			}
		}
	}
	return false
}

// convertToHCL recursively converts data structures to HCL format
func convertToHCL(categoryTypeMap yamlkit.ResourceCategoryTypeToNamesMap, key string, data interface{}, indent int) (string, bool, error) {
	switch v := data.(type) {
	case map[string]interface{}:
		return convertMapToHCL(categoryTypeMap, v, indent)
	case []interface{}:
		return convertSliceToHCL(categoryTypeMap, key, v, indent)
	case string:
		if isReference(categoryTypeMap, v) {
			return v, false, nil
		}
		return fmt.Sprintf(`"%s"`, escapeString(v)), false, nil
	case int:
		return strconv.Itoa(v), false, nil
	case int64:
		return strconv.FormatInt(v, 10), false, nil
	case float64:
		if v == float64(int64(v)) {
			return strconv.FormatInt(int64(v), 10), false, nil
		}
		return strconv.FormatFloat(v, 'f', -1, 64), false, nil
	case bool:
		return strconv.FormatBool(v), false, nil
	case nil:
		return "null", false, nil
	default:
		return "", false, fmt.Errorf("unsupported type: %T", v)
	}
}

// convertMapToHCL converts a map to HCL block format
func convertMapToHCL(categoryTypeMap yamlkit.ResourceCategoryTypeToNamesMap, m map[string]interface{}, indent int) (string, bool, error) {
	if len(m) == 0 {
		return "{}", false, nil
	}

	var lines []string
	indentStr := strings.Repeat("  ", indent)

	// Sort keys for consistent output
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// Check if this looks like an OpenTofu top-level block
	if isBlock(m) {
		return convertBlock(categoryTypeMap, m, indent)
	}

	// Regular map/object
	lines = append(lines, "{")

	for _, key := range keys {
		value := m[key]

		// Lists need to be converted back to blocks.
		// https://opentofu.org/docs/language/attr-as-blocks/

		hclValue, isBlock, err := convertToHCL(categoryTypeMap, key, value, indent+1)
		if err != nil {
			return "", false, err
		}

		// Handle nested objects and blocks with proper formatting
		if isBlock {
			lines = append(lines, fmt.Sprintf("%s  %s", indentStr, hclValue))
		} else {
			lines = append(lines, fmt.Sprintf("%s  %s = %s", indentStr, key, hclValue))
		}
	}

	lines = append(lines, indentStr+"}")
	return strings.Join(lines, "\n"), false, nil
}

// convertSliceToHCL converts a slice to HCL array format
func convertSliceToHCL(categoryTypeMap yamlkit.ResourceCategoryTypeToNamesMap, key string, s []interface{}, indent int) (string, bool, error) {
	if len(s) == 0 {
		return "[]", false, nil
	}

	var elements []string

	// Check if all elements are simple (non-nested)
	allSimple := true
	for _, item := range s {
		if isComplexType(item) {
			allSimple = false
			break
		}
	}

	if allSimple && len(s) <= 3 {
		// Inline simple arrays
		for _, item := range s {
			hclValue, _, err := convertToHCL(categoryTypeMap, "", item, indent)
			if err != nil {
				return "", false, err
			}
			elements = append(elements, hclValue)
		}
		return fmt.Sprintf("[%s]", strings.Join(elements, ", ")), false, nil
	}

	// Arrays of objects are converted to blocks.
	var lines []string
	for _, item := range s {
		hclValue, _, err := convertToHCL(categoryTypeMap, "", item, indent)
		if err != nil {
			return "", false, err
		}

		hclValue = key + " " + hclValue
		// Already indented in the recursive call.
		lines = append(lines, hclValue)
	}

	return strings.Join(lines, "\n"), true, nil
}

// isBlock checks if a map represents an OpenTofu block
func isBlock(m map[string]interface{}) bool {
	// TODO: decide what block types to support.
	// Look for ConfigHub metadata
	_, isBlock := m[MetadataPrefix]
	return isBlock
}

// convertBlock converts an OpenTofu block
func convertBlock(categoryTypeMap yamlkit.ResourceCategoryTypeToNamesMap, m map[string]interface{}, indent int) (string, bool, error) {
	indentStr := strings.Repeat("  ", indent)

	metadata, ok := m[MetadataPrefix]
	if !ok {
		return "", false, errors.New("block is missing metadata")
	}
	metadataMap, ok := metadata.(map[string]interface{})
	if !ok {
		return "", false, errors.New("block metadata is expected to be a map")
	}
	blockCategory, ok := metadataMap[BlockCategoryField]
	if !ok {
		return "", false, errors.New("block is missing category")
	}
	blockCategoryString, ok := blockCategory.(string)
	if !ok {
		return "", false, errors.New("block category is expected to be a string")
	}
	blockCategoryString = convertCategoryToBlockType(api.ResourceCategory(blockCategoryString))
	blockType, ok := metadataMap[BlockTypeField]
	if !ok {
		return "", false, errors.New("block is missing type")
	}
	blockName, ok := metadataMap[BlockNameField]
	if !ok {
		return "", false, errors.New("block is missing name")
	}
	delete(m, MetadataPrefix)

	var lines []string
	if blockName == BlockNameSingleton {
		lines = append(lines, fmt.Sprintf("%s%s {", indentStr, blockCategoryString))
	} else if blockType == blockCategory {
		lines = append(lines, fmt.Sprintf("%s%s \"%s\" {", indentStr, blockCategoryString, blockName))
	} else {
		lines = append(lines, fmt.Sprintf("%s%s \"%s\" \"%s\" {", indentStr, blockCategoryString, blockType, blockName))
	}

	// Sort keys for consistent output
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, key := range keys {
		value := m[key]
		hclValue, isBlock, err := convertToHCL(categoryTypeMap, key, value, indent+1)
		if err != nil {
			return "", false, err
		}

		if isBlock {
			lines = append(lines, fmt.Sprintf("%s  %s", indentStr, hclValue))
		} else {
			lines = append(lines, fmt.Sprintf("%s  %s = %s", indentStr, key, hclValue))
		}
	}

	lines = append(lines, indentStr+"}")
	return strings.Join(lines, "\n"), true, nil
}

// isComplexType checks if a value is a complex type (map or slice)
func isComplexType(v interface{}) bool {
	switch v.(type) {
	case map[string]interface{}, []interface{}:
		return true
	default:
		return false
	}
}

// escapeString escapes special characters in strings for HCL
func escapeString(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	s = strings.ReplaceAll(s, "\n", "\\n")
	s = strings.ReplaceAll(s, "\t", "\\t")
	s = strings.ReplaceAll(s, "\r", "\\r")
	return s
}

func (*HclResourceProviderType) YAMLToNative(yamlData []byte) ([]byte, error) {
	return YAMLToHCL(yamlData)
}
