// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

// Package inikit is used to interpret AppConfig/INI configuration units.
package inikit

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"

	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/constants"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/workerapi"
	"github.com/confighub/sdk/core/third_party/gaby"
	"github.com/go-ini/ini"
	"gopkg.in/yaml.v3"
)

// User data errors should not be logged here. They will be logged by the caller.
// Errors indicate that the operation could not be completed.
// Messages should be acceptable to return to the user, and should indicate the
// location of the problem in the configuration data.

type INIResourceProviderType struct {
	pathRegistry      api.AttributeNameToResourceTypeToPathToVisitorInfoType
	attributeRegistry api.AttributeNameToAttributeDescriptor
}

// NewINIResourceProvider creates a new INIResourceProviderType with its own path registry.
func NewINIResourceProvider() *INIResourceProviderType {
	return &INIResourceProviderType{
		pathRegistry:      make(api.AttributeNameToResourceTypeToPathToVisitorInfoType),
		attributeRegistry: make(api.AttributeNameToAttributeDescriptor),
	}
}

func (*INIResourceProviderType) MergeKeyForPath(_ api.ResourceType, _ string) (string, bool) {
	return "", false
}

func (rp *INIResourceProviderType) GetPathRegistry() api.AttributeNameToResourceTypeToPathToVisitorInfoType {
	return rp.pathRegistry
}

func (rp *INIResourceProviderType) GetAttributeRegistry() api.AttributeNameToAttributeDescriptor {
	return rp.attributeRegistry
}

// DefaultResourceCategory returns the default resource category to assume, which is AppConfig in this case.
func (*INIResourceProviderType) DefaultResourceCategory() api.ResourceCategory {
	return api.ResourceCategoryAppConfig
}

// ResourceCategoryGetter just returns ResourceCategoryAppConfig for INI documents.
func (*INIResourceProviderType) ResourceCategoryGetter(doc *gaby.YamlDoc) (api.ResourceCategory, error) {
	// TODO: check that the document is non-empty?
	return api.ResourceCategoryAppConfig, nil
}

const (
	ResourceTypeNoSchema = api.ResourceType("NoSchema")
	ResourceNameNoName   = api.ResourceName("NoName")
	ConfigSchemaPath     = api.ResolvedPath("configHub.configSchema")
	ConfigNamePath       = api.ResolvedPath("configHub.configName")
)

// ResourceTypeGetter extracts the property configHub.configSchema, and returns NoSchema if not present.
func (*INIResourceProviderType) ResourceTypeGetter(doc *gaby.YamlDoc) (api.ResourceType, error) {
	// TODO: Decide how to use this. It would be useful to be able to distinguish different
	// app schemas from one another.
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
func (*INIResourceProviderType) ResourceNameGetter(doc *gaby.YamlDoc) (api.ResourceName, error) {
	// TODO: Decide how to use this. It would be useful to be able to distinguish different
	// files for different purposes from one another.
	name, hasName, err := yamlkit.YamlSafePathGetValue[string](doc, ConfigNamePath, true)
	if err != nil {
		return "", err
	}
	if hasName {
		return api.ResourceName(name), nil
	}
	return ResourceNameNoName, nil
}

func (*INIResourceProviderType) ScopelessResourceNamePath() api.ResolvedPath {
	return ConfigNamePath
}

func (*INIResourceProviderType) SetResourceName(doc *gaby.YamlDoc, name string) error {
	_, err := doc.SetP(name, string(ConfigNamePath))
	return err
}

func (rp *INIResourceProviderType) ResourceIDGetter(doc *gaby.YamlDoc) (string, error) {
	resourceIDPath := rp.ContextPath(constants.ResourceIDKeySuffix)
	id, found, err := yamlkit.YamlSafePathGetValue[string](doc, api.ResolvedPath(resourceIDPath), true)
	if err != nil || !found {
		return "", err
	}
	return id, nil
}

func (rp *INIResourceProviderType) SetResourceID(doc *gaby.YamlDoc, id string) error {
	resourceIDPath := rp.ContextPath(constants.ResourceIDKeySuffix)
	_, err := doc.SetP(id, resourceIDPath)
	return err
}

func (*INIResourceProviderType) TypeDescription() string {
	return "Schema"
}

const nameSeparatorString = ""

func (*INIResourceProviderType) NormalizeName(name string) string {
	// Virtually all characters are valid
	return name
}

func (*INIResourceProviderType) NameSeparator() string {
	return nameSeparatorString
}

const (
	contextPathPrefix = "configHub."
)

func (*INIResourceProviderType) ContextPath(contextField string) string {
	return contextPathPrefix + yamlkit.LowerFirst(contextField)
}

// ResourceAndCategoryTypeMaps returns maps of all resources in the provided list of parsed YAML
// documents, from from names to categories+types and categories+types to names.
func (rp *INIResourceProviderType) ResourceAndCategoryTypeMaps(docs gaby.Container) (resourceMap yamlkit.ResourceNameToCategoryTypesMap, categoryTypeMap yamlkit.ResourceCategoryTypeToNamesMap, err error) {
	return yamlkit.ResourceAndCategoryTypeMaps(docs, rp)
}

func (*INIResourceProviderType) RemoveScopeFromResourceName(resourceName api.ResourceName) api.ResourceName {
	return resourceName
}

func (*INIResourceProviderType) ResourceTypesAreSimilar(resourceTypeA, resourceTypeB api.ResourceType) bool {
	return resourceTypeA == resourceTypeB
}

func (*INIResourceProviderType) DataType() api.DataType {
	return api.DataTypeINI
}

func (*INIResourceProviderType) GetToolchainType() workerapi.ToolchainType {
	return workerapi.ToolchainAppConfigINI
}

// parseINIValue attempts to convert an INI value string to the appropriate type.
// It tries in order: bool, int, and falls back to string.
func parseINIValue(value string) interface{} {
	// Try boolean
	if value == "true" {
		return true
	}
	if value == "false" {
		return false
	}

	// Try integer
	if intVal, err := strconv.ParseInt(value, 10, 64); err == nil {
		return int(intVal)
	}

	// Default to string
	return value
}

// setNestedValue sets a value in a nested map structure using a dot-separated path.
// For example, path "database.ssl" with key "enabled" and value false creates:
// map[database:map[ssl:map[enabled:false]]]
func setNestedValue(result map[string]interface{}, path string, key string, value interface{}) {
	if path == "" {
		result[key] = value
		return
	}

	parts := strings.Split(path, ".")
	current := result

	// Navigate/create the nested structure
	for _, part := range parts {
		if _, exists := current[part]; !exists {
			current[part] = make(map[string]interface{})
		}
		// Move deeper into the structure
		if nextMap, ok := current[part].(map[string]interface{}); ok {
			current = nextMap
		} else {
			// If it's not a map, we can't navigate further - this shouldn't happen in normal cases
			return
		}
	}

	// Set the final value
	current[key] = value
}

// NativeToYAML converts INI data to YAML format using native libraries.
func (*INIResourceProviderType) NativeToYAML(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return []byte{}, nil
	}

	// Parse INI data
	cfg, err := ini.Load(data)
	if err != nil {
		return nil, fmt.Errorf("error parsing INI: %w", err)
	}

	// Convert INI sections to nested map structure
	result := make(map[string]interface{})

	for _, section := range cfg.Sections() {
		sectionName := section.Name()

		for _, key := range section.Keys() {
			// Parse the value to get the appropriate type
			value := parseINIValue(key.Value())

			if sectionName == ini.DefaultSection {
				// For default section, add keys directly to root
				result[key.Name()] = value
			} else {
				// For named sections, parse the dotted path and create nested structure
				setNestedValue(result, sectionName, key.Name(), value)
			}
		}
	}

	// Convert to YAML
	var output bytes.Buffer
	encoder := yaml.NewEncoder(&output)
	encoder.SetIndent(2)
	if err := encoder.Encode(result); err != nil {
		return nil, fmt.Errorf("error converting INI to YAML: %w", err)
	}
	if err := encoder.Close(); err != nil {
		return nil, fmt.Errorf("error finalizing YAML encoding: %w", err)
	}

	return output.Bytes(), nil
}

// addINISection recursively adds keys and nested sections to the INI config.
// It builds section names like "parent.child.grandchild" for nested maps.
func addINISection(cfg *ini.File, sectionName string, data map[string]interface{}) error {
	// First, check if this section has any direct (non-map) keys
	hasDirectKeys := false
	for _, value := range data {
		if _, isMap := value.(map[string]interface{}); !isMap {
			hasDirectKeys = true
			break
		}
	}

	// Only create the section if it has direct keys
	var section *ini.Section
	if hasDirectKeys {
		var err error
		section, err = cfg.NewSection(sectionName)
		if err != nil {
			return fmt.Errorf("error creating section %s: %w", sectionName, err)
		}
	}

	// Process all keys
	for key, value := range data {
		switch v := value.(type) {
		case map[string]interface{}:
			// This is a nested section - recurse with dotted section name
			nestedSectionName := sectionName + "." + key
			if err := addINISection(cfg, nestedSectionName, v); err != nil {
				return err
			}
		default:
			// This is a regular key-value pair - add to current section
			if section == nil {
				return fmt.Errorf("internal error: section is nil for key %s.%s", sectionName, key)
			}
			valueStr := fmt.Sprintf("%v", v)
			_, err := section.NewKey(key, valueStr)
			if err != nil {
				return fmt.Errorf("error creating key %s.%s: %w", sectionName, key, err)
			}
		}
	}

	return nil
}

// YAMLToNative converts YAML data to INI format using native libraries.
func (*INIResourceProviderType) YAMLToNative(yamlData []byte) ([]byte, error) {
	if len(yamlData) == 0 {
		return []byte{}, nil
	}

	// Parse YAML into a generic map
	var data map[string]interface{}
	if err := yaml.Unmarshal(yamlData, &data); err != nil {
		return nil, fmt.Errorf("error parsing YAML: %w", err)
	}

	// Create INI file
	cfg := ini.Empty()

	// Convert map to INI sections
	for key, value := range data {
		switch v := value.(type) {
		case map[string]interface{}:
			// This is a section - process it recursively
			if err := addINISection(cfg, key, v); err != nil {
				return nil, err
			}
		default:
			// This is a root-level key
			_, err := cfg.Section(ini.DefaultSection).NewKey(key, fmt.Sprintf("%v", v))
			if err != nil {
				return nil, fmt.Errorf("error creating root key %s: %w", key, err)
			}
		}
	}

	// Write to buffer
	var output bytes.Buffer
	if _, err := cfg.WriteTo(&output); err != nil {
		return nil, fmt.Errorf("error converting YAML to INI: %w", err)
	}

	return output.Bytes(), nil
}
