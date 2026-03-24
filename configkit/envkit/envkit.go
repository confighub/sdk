// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

// Package envkit is used to interpret AppConfig/Env configuration units.
package envkit

import (
	"bufio"
	"bytes"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/constants"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/workerapi"
	"github.com/confighub/sdk/core/third_party/gaby"
	"gopkg.in/yaml.v3"
)

// User data errors should not be logged here. They will be logged by the caller.
// Errors indicate that the operation could not be completed.
// Messages should be acceptable to return to the user, and should indicate the
// location of the problem in the configuration data.

type EnvResourceProviderType struct {
	pathRegistry      api.AttributeNameToResourceTypeToPathToVisitorInfoType
	attributeRegistry api.AttributeNameToAttributeDescriptor
}

// NewEnvResourceProvider creates a new EnvResourceProviderType with its own path registry.
func NewEnvResourceProvider() *EnvResourceProviderType {
	return &EnvResourceProviderType{
		pathRegistry:      make(api.AttributeNameToResourceTypeToPathToVisitorInfoType),
		attributeRegistry: make(api.AttributeNameToAttributeDescriptor),
	}
}

func (rp *EnvResourceProviderType) GetPathRegistry() api.AttributeNameToResourceTypeToPathToVisitorInfoType {
	return rp.pathRegistry
}

func (rp *EnvResourceProviderType) GetAttributeRegistry() api.AttributeNameToAttributeDescriptor {
	return rp.attributeRegistry
}

func (*EnvResourceProviderType) MergeKeyForPath(_ api.ResourceType, _ string) (string, bool) {
	return "", false
}

// DefaultResourceCategory returns the default resource category to assume, which is AppConfig in this case.
func (*EnvResourceProviderType) DefaultResourceCategory() api.ResourceCategory {
	return api.ResourceCategoryAppConfig
}

// ResourceCategoryGetter just returns ResourceCategoryAppConfig for Env documents.
func (*EnvResourceProviderType) ResourceCategoryGetter(doc *gaby.YamlDoc) (api.ResourceCategory, error) {
	return api.ResourceCategoryAppConfig, nil
}

const (
	ResourceTypeNoSchema = api.ResourceType("NoSchema")
	ResourceNameNoName   = api.ResourceName("NoName")
	ConfigSchemaPath     = api.ResolvedPath("configHub.configSchema")
	ConfigNamePath       = api.ResolvedPath("configHub.configName")
)

// ResourceTypeGetter extracts the property configHub.configSchema, and returns NoSchema if not present.
func (*EnvResourceProviderType) ResourceTypeGetter(doc *gaby.YamlDoc) (api.ResourceType, error) {
	schemaType, hasSchema, err := yamlkit.YamlSafePathGetValue[string](doc, ConfigSchemaPath, true)
	if err != nil {
		return "", err
	}
	if hasSchema {
		return api.ResourceType(schemaType), nil
	}
	return ResourceTypeNoSchema, nil
}

// ResourceNameGetter extracts the property configHub.configName, and returns NoName if not present.
func (*EnvResourceProviderType) ResourceNameGetter(doc *gaby.YamlDoc) (api.ResourceName, error) {
	name, hasName, err := yamlkit.YamlSafePathGetValue[string](doc, ConfigNamePath, true)
	if err != nil {
		return "", err
	}
	if hasName {
		return api.ResourceName(name), nil
	}
	return ResourceNameNoName, nil
}

func (*EnvResourceProviderType) ScopelessResourceNamePath() api.ResolvedPath {
	return ConfigNamePath
}

func (*EnvResourceProviderType) SetResourceName(doc *gaby.YamlDoc, name string) error {
	_, err := doc.SetP(name, string(ConfigNamePath))
	return err
}

func (rp *EnvResourceProviderType) ResourceIDGetter(doc *gaby.YamlDoc) (string, error) {
	resourceIDPath := rp.ContextPath(constants.ResourceIDKeySuffix)
	id, found, err := yamlkit.YamlSafePathGetValue[string](doc, api.ResolvedPath(resourceIDPath), true)
	if err != nil || !found {
		return "", err
	}
	return id, nil
}

func (rp *EnvResourceProviderType) SetResourceID(doc *gaby.YamlDoc, id string) error {
	resourceIDPath := rp.ContextPath(constants.ResourceIDKeySuffix)
	_, err := doc.SetP(id, resourceIDPath)
	return err
}

func (rp *EnvResourceProviderType) DeleteResourceID(doc *gaby.YamlDoc) error {
	resourceIDPath := rp.ContextPath(constants.ResourceIDKeySuffix)
	return doc.DeleteP(resourceIDPath)
}

func (*EnvResourceProviderType) TypeDescription() string {
	return "Schema"
}

const nameSeparatorString = ""

func (*EnvResourceProviderType) NormalizeName(name string) string {
	return name
}

func (*EnvResourceProviderType) NameSeparator() string {
	return nameSeparatorString
}

const (
	contextPathPrefx = "configHub."
)

func (*EnvResourceProviderType) ContextPath(contextField string) string {
	return contextPathPrefx + yamlkit.LowerFirst(contextField)
}

// ResourceAndCategoryTypeMaps returns maps of all resources in the provided list of parsed YAML
// documents, from from names to categories+types and categories+types to names.
func (rp *EnvResourceProviderType) ResourceAndCategoryTypeMaps(docs gaby.Container) (resourceMap yamlkit.ResourceNameToCategoryTypesMap, categoryTypeMap yamlkit.ResourceCategoryTypeToNamesMap, err error) {
	return yamlkit.ResourceAndCategoryTypeMaps(docs, rp)
}

func (*EnvResourceProviderType) RemoveScopeFromResourceName(resourceName api.ResourceName) api.ResourceName {
	return resourceName
}

func (*EnvResourceProviderType) ResourceTypesAreSimilar(resourceTypeA, resourceTypeB api.ResourceType) bool {
	return resourceTypeA == resourceTypeB
}

func (*EnvResourceProviderType) DataType() api.DataType {
	return api.DataTypeEnv
}

func (*EnvResourceProviderType) GetToolchainType() workerapi.ToolchainType {
	return workerapi.ToolchainAppConfigEnv
}

// EnvEntry represents a single key-value pair from an env file.
type EnvEntry struct {
	Key   string
	Value string
}

// ParseEnvEntries parses env file data into key-value string pairs, skipping
// empty lines, comments, and configHub metadata keys. Values are kept as raw strings.
func ParseEnvEntries(data []byte) ([]EnvEntry, error) {
	var entries []EnvEntry
	scanner := bufio.NewScanner(bytes.NewReader(data))
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		eqIndex := strings.Index(line, "=")
		if eqIndex == -1 {
			return nil, fmt.Errorf("error parsing line %d: no '=' separator found", lineNum)
		}
		key := strings.TrimSpace(line[:eqIndex])
		value := strings.TrimSpace(line[eqIndex+1:])
		value = stripQuotes(value)
		if key == "" {
			continue
		}
		// Skip configHub metadata keys
		if strings.HasPrefix(key, contextPathPrefx) {
			continue
		}
		entries = append(entries, EnvEntry{Key: key, Value: value})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading env data: %w", err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Key < entries[j].Key })
	return entries, nil
}

// NativeToYAML converts env file data (KEY=value lines) to YAML format.
// Env files use a flat KEY=value format. Non-configHub keys must not contain
// dots, since shells such as bash do not support dots in environment variable
// names. Keys under the configHub metadata prefix (e.g., configHub.configName)
// are allowed to contain dots because they are removed before rendering.
func (*EnvResourceProviderType) NativeToYAML(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return []byte{}, nil
	}

	result := make(map[string]interface{})

	scanner := bufio.NewScanner(bytes.NewReader(data))
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Find the first = separator
		eqIndex := strings.Index(line, "=")
		if eqIndex == -1 {
			return nil, fmt.Errorf("error parsing line %d: no '=' separator found", lineNum)
		}

		key := strings.TrimSpace(line[:eqIndex])
		value := strings.TrimSpace(line[eqIndex+1:])

		// Strip surrounding quotes from value
		value = stripQuotes(value)

		if key == "" {
			continue
		}

		// configHub metadata keys may contain dots for nesting; all other keys must not.
		if strings.Contains(key, ".") && !strings.HasPrefix(key, contextPathPrefx) {
			return nil, fmt.Errorf("error parsing line %d: key %q contains dots, which are not valid in environment variable names; only keys under the %q prefix may use dots", lineNum, key, strings.TrimSuffix(contextPathPrefx, "."))
		}

		// configHub keys use dot notation for nesting; other keys are flat.
		setNestedValue(result, key, convertValue(value))
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading env data: %w", err)
	}

	var buf bytes.Buffer
	encoder := yaml.NewEncoder(&buf)
	encoder.SetIndent(2)
	if err := encoder.Encode(result); err != nil {
		return nil, fmt.Errorf("error encoding YAML: %w", err)
	}
	if err := encoder.Close(); err != nil {
		return nil, fmt.Errorf("error closing YAML encoder: %w", err)
	}

	return buf.Bytes(), nil
}

// YAMLToNative converts YAML data to env file format (KEY=value lines).
func (*EnvResourceProviderType) YAMLToNative(yamlData []byte) ([]byte, error) {
	if len(yamlData) == 0 {
		return []byte{}, nil
	}

	var data interface{}
	if err := yaml.Unmarshal(yamlData, &data); err != nil {
		return nil, fmt.Errorf("error parsing YAML: %w", err)
	}

	props := make(map[string]string)
	flattenMap(data, "", props)

	// Validate that non-configHub keys don't contain dots (env vars can't have dots).
	for key := range props {
		if strings.Contains(key, ".") && !strings.HasPrefix(key, contextPathPrefx) {
			return nil, fmt.Errorf("key %q contains dots, which are not valid in environment variable names; only keys under the %q prefix may use dots", key, strings.TrimSuffix(contextPathPrefx, "."))
		}
	}

	// Sort keys for consistent output
	keys := make([]string, 0, len(props))
	for key := range props {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var buf bytes.Buffer
	writer := bufio.NewWriter(&buf)
	for _, key := range keys {
		value := props[key]
		// Quote values that contain spaces or special characters
		if needsQuoting(value) {
			fmt.Fprintf(writer, "%s=\"%s\"\n", key, escapeValue(value))
		} else {
			fmt.Fprintf(writer, "%s=%s\n", key, value)
		}
	}
	if err := writer.Flush(); err != nil {
		return nil, fmt.Errorf("error flushing buffer: %w", err)
	}

	return buf.Bytes(), nil
}

// stripQuotes removes surrounding single or double quotes from a value.
func stripQuotes(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

// setNestedValue sets a value in a nested map structure based on dot notation.
func setNestedValue(m map[string]interface{}, key string, value interface{}) {
	parts := strings.Split(key, ".")
	current := m

	for _, part := range parts[:len(parts)-1] {
		if existing, exists := current[part]; exists {
			if nestedMap, ok := existing.(map[string]interface{}); ok {
				current = nestedMap
			} else {
				newMap := make(map[string]interface{})
				newMap[""] = existing
				current[part] = newMap
				current = newMap
			}
		} else {
			newMap := make(map[string]interface{})
			current[part] = newMap
			current = newMap
		}
	}

	current[parts[len(parts)-1]] = value
}

// convertValue attempts to convert string values to appropriate types.
func convertValue(value string) interface{} {
	if value == "true" || value == "false" {
		return value == "true"
	}
	if intVal, err := strconv.ParseInt(value, 10, 64); err == nil {
		return intVal
	}
	return value
}

// flattenMap recursively flattens nested structures into dot-notation keys.
func flattenMap(data interface{}, prefix string, result map[string]string) {
	switch v := data.(type) {
	case map[string]interface{}:
		for key, value := range v {
			newKey := key
			if prefix != "" {
				newKey = prefix + "." + key
			}
			flattenMap(value, newKey, result)
		}
	case map[interface{}]interface{}:
		for key, value := range v {
			keyStr := fmt.Sprintf("%v", key)
			newKey := keyStr
			if prefix != "" {
				newKey = prefix + "." + keyStr
			}
			flattenMap(value, newKey, result)
		}
	case []interface{}:
		for i, item := range v {
			newKey := strconv.Itoa(i)
			if prefix != "" {
				newKey = prefix + "." + newKey
			}
			flattenMap(item, newKey, result)
		}
	default:
		result[prefix] = fmt.Sprintf("%v", v)
	}
}

// needsQuoting returns true if a value contains characters that need quoting.
func needsQuoting(value string) bool {
	return strings.ContainsAny(value, " \t\"'\\#$")
}

// escapeValue escapes special characters in a value for env file format.
func escapeValue(value string) string {
	value = strings.ReplaceAll(value, "\\", "\\\\")
	value = strings.ReplaceAll(value, "\"", "\\\"")
	return value
}
