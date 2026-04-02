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
	orderedmap "github.com/wk8/go-ordered-map/v2"
)

// User data errors should not be logged here. They will be logged by the caller.
// Errors indicate that the operation could not be completed.
// Messages should be acceptable to return to the user, and should indicate the
// location of the problem in the configuration data.

type INIResourceProviderType struct {
	yamlkit.ResourceProviderRegistry
}

// NewINIResourceProvider creates a new INIResourceProviderType with its own path registry.
func NewINIResourceProvider() *INIResourceProviderType {
	return &INIResourceProviderType{
		ResourceProviderRegistry: yamlkit.NewResourceProviderRegistry(),
	}
}

func (*INIResourceProviderType) MergeKeyForPath(_ api.ResourceType, _ string) (string, bool) {
	return "", false
}

func (*INIResourceProviderType) IsMapKeyPath(_ api.ResourceType, _ string) bool {
	return false
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

func (rp *INIResourceProviderType) ResourceNameStableCoreGetter(doc *gaby.YamlDoc) (api.ResourceName, error) {
	resourceNameStableCorePath := rp.ContextPath(constants.ResourceNameStableCoreKeySuffix)
	name, found, err := yamlkit.YamlSafePathGetValue[string](doc, api.ResolvedPath(resourceNameStableCorePath), true)
	if err != nil || !found {
		return "", err
	}
	return api.ResourceName(name), nil
}

// Deprecated: Use ResourceMergeIDGetter instead.
func (rp *INIResourceProviderType) ResourceIDGetter(doc *gaby.YamlDoc) (string, error) {
	resourceIDPath := rp.ContextPath(constants.ResourceIDKeySuffix)
	id, found, err := yamlkit.YamlSafePathGetValue[string](doc, api.ResolvedPath(resourceIDPath), true)
	if err != nil || !found {
		return "", err
	}
	return id, nil
}

// Deprecated: Use SetResourceMergeID instead.
func (rp *INIResourceProviderType) SetResourceID(doc *gaby.YamlDoc, id string) error {
	resourceIDPath := rp.ContextPath(constants.ResourceIDKeySuffix)
	_, err := doc.SetP(id, resourceIDPath)
	return err
}

// Deprecated: Use DeleteResourceMergeID instead.
func (rp *INIResourceProviderType) DeleteResourceID(doc *gaby.YamlDoc) error {
	resourceIDPath := rp.ContextPath(constants.ResourceIDKeySuffix)
	return doc.DeleteP(resourceIDPath)
}

func (rp *INIResourceProviderType) ResourceMergeIDGetter(doc *gaby.YamlDoc) (string, error) {
	resourceMergeIDPath := rp.ContextPath(constants.ResourceMergeIDKeySuffix)
	id, found, err := yamlkit.YamlSafePathGetValue[string](doc, api.ResolvedPath(resourceMergeIDPath), true)
	if err != nil {
		return "", err
	}
	if found {
		return id, nil
	}
	// Fall back to legacy ResourceID path for backward compatibility.
	return rp.ResourceIDGetter(doc)
}

func (rp *INIResourceProviderType) SetResourceMergeID(doc *gaby.YamlDoc, id string) error {
	resourceMergeIDPath := rp.ContextPath(constants.ResourceMergeIDKeySuffix)
	_, err := doc.SetP(id, resourceMergeIDPath)
	return err
}

func (rp *INIResourceProviderType) DeleteResourceMergeID(doc *gaby.YamlDoc) error {
	resourceMergeIDPath := rp.ContextPath(constants.ResourceMergeIDKeySuffix)
	_ = doc.DeleteP(resourceMergeIDPath)
	// Also delete legacy ResourceID path.
	return rp.DeleteResourceID(doc)
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


// NativeToYAML converts INI data to YAML format using native libraries.
// Comments from the INI source are preserved as $comment$ map keys in the output.
// Key order from the INI source is preserved by building a gaby YamlDoc directly.
func (*INIResourceProviderType) NativeToYAML(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return []byte{}, nil
	}

	// Parse INI data (go-ini preserves section and key order)
	cfg, err := ini.Load(data)
	if err != nil {
		return nil, fmt.Errorf("error parsing INI: %w", err)
	}

	// Extract comments from raw INI source
	comments := extractINIComments(data)

	// Build a gaby YamlDoc directly, preserving source key order.
	doc := gaby.New()

	for _, section := range cfg.Sections() {
		sectionName := section.Name()

		if sectionName != ini.DefaultSection {
			// Add section head comment if present
			parts := strings.Split(sectionName, ".")
			targetKey := parts[len(parts)-1]
			parentPath := strings.Join(parts[:len(parts)-1], ".")
			sectionHeadKey := yamlkit.CommentKey(yamlkit.CommentHead, targetKey)
			if text, ok := comments[flatCommentPath(parentPath, sectionHeadKey)]; ok {
				doc.Set(text, appendPath(parentPath, sectionHeadKey)...)
				delete(comments, flatCommentPath(parentPath, sectionHeadKey))
			}
		}

		for _, key := range section.Keys() {
			value := parseINIValue(key.Value())

			var dataPath []string
			var commentParentPath string
			if sectionName == ini.DefaultSection {
				dataPath = []string{key.Name()}
				commentParentPath = ""
			} else {
				dataPath = append(strings.Split(sectionName, "."), key.Name())
				commentParentPath = sectionName
			}

			// Add head comment for this key
			headKey := yamlkit.CommentKey(yamlkit.CommentHead, key.Name())
			flatHead := flatCommentPath(commentParentPath, headKey)
			if text, ok := comments[flatHead]; ok {
				doc.Set(text, appendPath(commentParentPath, headKey)...)
				delete(comments, flatHead)
			}

			// Add the data value
			doc.Set(value, dataPath...)

			// Add line comment for this key
			lineKey := yamlkit.CommentKey(yamlkit.CommentLine, key.Name())
			flatLine := flatCommentPath(commentParentPath, lineKey)
			if text, ok := comments[flatLine]; ok {
				doc.Set(text, appendPath(commentParentPath, lineKey)...)
				delete(comments, flatLine)
			}
		}
	}

	// Add any remaining comments (e.g., trailing foot comments)
	for path, text := range comments {
		doc.Set(text, strings.Split(path, ".")...)
	}

	return doc.Bytes(), nil
}

// flatCommentPath builds a dot-separated comment path from a parent path and comment key.
func flatCommentPath(parentPath, commentKey string) string {
	if parentPath != "" {
		return parentPath + "." + commentKey
	}
	return commentKey
}

// appendPath splits a parent path and appends a key, returning the path hierarchy.
func appendPath(parentPath, key string) []string {
	if parentPath != "" {
		return append(strings.Split(parentPath, "."), key)
	}
	return []string{key}
}

// addINISectionOrdered recursively adds keys and nested sections to the INI config,
// preserving the key order from the ordered map.
func addINISectionOrdered(cfg *ini.File, sectionName string, data *orderedmap.OrderedMap[string, interface{}]) error {
	// First, check if this section has any direct (non-map) keys
	hasDirectKeys := false
	for pair := data.Oldest(); pair != nil; pair = pair.Next() {
		if _, isMap := pair.Value.(*orderedmap.OrderedMap[string, interface{}]); !isMap {
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

	// Process keys in order
	for pair := data.Oldest(); pair != nil; pair = pair.Next() {
		key := pair.Key
		switch v := pair.Value.(type) {
		case *orderedmap.OrderedMap[string, interface{}]:
			// This is a nested section - recurse with dotted section name
			nestedSectionName := sectionName + "." + key
			if err := addINISectionOrdered(cfg, nestedSectionName, v); err != nil {
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
// If the YAML contains $comment$ map keys, they are converted back to INI comments.
// Key order from the YAML source is preserved.
func (*INIResourceProviderType) YAMLToNative(yamlData []byte) ([]byte, error) {
	if len(yamlData) == 0 {
		return []byte{}, nil
	}

	// Parse YAML with gaby to preserve key order
	doc, err := gaby.ParseYAML(yamlData)
	if err != nil {
		return nil, fmt.Errorf("error parsing YAML: %w", err)
	}

	// Extract comment keys from ordered data
	cleanData, comments := yamlkit.ExtractCommentsFromData(doc.DataOrdered())
	cleanMap, ok := cleanData.(*orderedmap.OrderedMap[string, interface{}])
	if !ok {
		return nil, fmt.Errorf("unexpected data type after extracting comments: %T", cleanData)
	}

	// Create INI file
	cfg := ini.Empty()

	// Convert ordered map to INI sections, preserving key order
	for pair := cleanMap.Oldest(); pair != nil; pair = pair.Next() {
		key := pair.Key
		switch v := pair.Value.(type) {
		case *orderedmap.OrderedMap[string, interface{}]:
			// This is a section - process it recursively
			if err := addINISectionOrdered(cfg, key, v); err != nil {
				return nil, err
			}
		default:
			// This is a root-level key
			_, err := cfg.Section(ini.DefaultSection).NewKey(key, fmt.Sprintf("%v", pair.Value))
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

	// Re-inject comments into INI output
	result := injectINIComments(output.Bytes(), comments)

	return result, nil
}
