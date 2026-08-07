// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

// Package tomlkit is used to interpret AppConfig/TOML configuration units.
package tomlkit

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/constants"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/workerapi"
	"github.com/confighub/sdk/core/third_party/gaby"
)

// User data errors should not be logged here. They will be logged by the caller.
// Errors indicate that the operation could not be completed.
// Messages should be acceptable to return to the user, and should indicate the
// location of the problem in the configuration data.

type TOMLResourceProviderType struct {
	yamlkit.ResourceProviderRegistry
}

// NewTOMLResourceProvider creates a new TOMLResourceProviderType with its own path registry.
func NewTOMLResourceProvider() *TOMLResourceProviderType {
	return &TOMLResourceProviderType{
		ResourceProviderRegistry: yamlkit.NewResourceProviderRegistry(),
	}
}

func (*TOMLResourceProviderType) MergeKeysForPath(_ api.ResourceType, _ string) ([]string, bool) {
	return nil, false
}

// ExclusiveFieldsForPath returns no union: this format has no schema declaring one.
func (*TOMLResourceProviderType) ExclusiveFieldsForPath(_ api.ResourceType, _ string) (yamlkit.ExclusiveFields, bool) {
	return yamlkit.ExclusiveFields{}, false
}

func (*TOMLResourceProviderType) IsMapKeyPath(_ api.ResourceType, _ string) bool {
	return false
}

// DefaultResourceCategory returns the default resource category to assume, which is AppConfig in this case.
func (*TOMLResourceProviderType) DefaultResourceCategory() api.ResourceCategory {
	return api.ResourceCategoryAppConfig
}

// ResourceCategoryGetter just returns ResourceCategoryAppConfig for TOML documents.
func (*TOMLResourceProviderType) ResourceCategoryGetter(doc *gaby.YamlDoc) (api.ResourceCategory, error) {
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
func (*TOMLResourceProviderType) ResourceTypeGetter(doc *gaby.YamlDoc) (api.ResourceType, error) {
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
func (*TOMLResourceProviderType) ResourceNameGetter(doc *gaby.YamlDoc) (api.ResourceName, error) {
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

func (*TOMLResourceProviderType) ScopelessResourceNamePath() api.ResolvedPath {
	return ConfigNamePath
}

func (*TOMLResourceProviderType) SetResourceName(doc *gaby.YamlDoc, name string) error {
	_, err := doc.SetP(name, string(ConfigNamePath))
	return err
}

func (rp *TOMLResourceProviderType) ResourceNameStableCoreGetter(doc *gaby.YamlDoc) (api.ResourceName, error) {
	resourceNameStableCorePath := rp.ContextPath(constants.ResourceNameStableCoreKeySuffix)
	name, found, err := yamlkit.YamlSafePathGetValue[string](doc, api.ResolvedPath(resourceNameStableCorePath), true)
	if err != nil || !found {
		return "", err
	}
	return api.ResourceName(name), nil
}


func (*TOMLResourceProviderType) TypeDescription() string {
	return "Schema"
}

const nameSeparatorString = ""

func (*TOMLResourceProviderType) NormalizeName(name string) string {
	// Virtually all characters are valid
	return name
}

func (*TOMLResourceProviderType) NameSeparator() string {
	return nameSeparatorString
}

const (
	contextPathPrefix = "configHub."
)

func (*TOMLResourceProviderType) ContextPath(contextField string) string {
	return contextPathPrefix + yamlkit.LowerFirst(contextField)
}

// ResourceAndCategoryTypeMaps returns maps of all resources in the provided list of parsed YAML
// documents, from from names to categories+types and categories+types to names.
func (rp *TOMLResourceProviderType) ResourceAndCategoryTypeMaps(docs gaby.Container) (resourceMap yamlkit.ResourceNameToCategoryTypesMap, categoryTypeMap yamlkit.ResourceCategoryTypeToNamesMap, err error) {
	return yamlkit.ResourceAndCategoryTypeMaps(docs, rp)
}

func (*TOMLResourceProviderType) RemoveScopeFromResourceName(resourceName api.ResourceName) api.ResourceName {
	return resourceName
}

func (*TOMLResourceProviderType) ResourceTypesAreSimilar(resourceTypeA, resourceTypeB api.ResourceType) bool {
	return resourceTypeA == resourceTypeB
}

func (*TOMLResourceProviderType) DataType() api.DataType {
	return api.DataTypeTOML
}

func (*TOMLResourceProviderType) GetToolchainType() workerapi.ToolchainType {
	return workerapi.ToolchainAppConfigTOML
}

// NativeToYAML converts TOML data to YAML format using native libraries.
// Comments from the TOML source are preserved as $comment$ map keys in the output.
// Key order from the TOML source is preserved.
func (*TOMLResourceProviderType) NativeToYAML(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return []byte{}, nil
	}

	// Parse TOML with Decode to get MetaData (preserves source key order)
	var tomlData interface{}
	meta, err := toml.Decode(string(data), &tomlData)
	if err != nil {
		return nil, fmt.Errorf("error parsing TOML: %w", err)
	}

	// Convert typed slices ([]map[string]any → []any) for uniform handling
	tomlData = convertTypedSlices(tomlData)

	// Extract comments from raw TOML source
	comments := extractTOMLComments(data)

	// Build a gaby YamlDoc directly using MetaData key order
	doc := gaby.New()

	for _, key := range meta.Keys() {
		path := []string(key)
		targetKey := path[len(path)-1]
		parentPath := strings.Join(path[:len(path)-1], ".")

		// Add head comment for this key
		headCK := yamlkit.CommentKey(yamlkit.CommentHead, targetKey)
		flatHead := flatCommentPath(parentPath, headCK)
		if text, ok := comments[flatHead]; ok {
			doc.Set(text, splitPath(flatHead)...)
			delete(comments, flatHead)
		}

		// Look up the value in the decoded data
		value := lookupValue(tomlData, path)
		// Only set leaf values (skip tables/maps — gaby creates them implicitly)
		if !isTable(value) {
			doc.Set(value, path...)
		}

		// Add line comment for this key
		lineCK := yamlkit.CommentKey(yamlkit.CommentLine, targetKey)
		flatLine := flatCommentPath(parentPath, lineCK)
		if text, ok := comments[flatLine]; ok {
			doc.Set(text, splitPath(flatLine)...)
			delete(comments, flatLine)
		}
	}

	// Add any remaining comments (e.g., trailing foot comments)
	for path, text := range comments {
		doc.Set(text, splitPath(path)...)
	}

	return doc.Bytes(), nil
}

func flatCommentPath(parentPath, commentKey string) string {
	if parentPath != "" {
		return parentPath + "." + commentKey
	}
	return commentKey
}

func splitPath(dotPath string) []string {
	return strings.Split(dotPath, ".")
}

// lookupValue navigates a nested map structure to find the value at the given path.
func lookupValue(data interface{}, path []string) interface{} {
	current := data
	for _, segment := range path {
		m, ok := current.(map[string]interface{})
		if !ok {
			return nil
		}
		current = m[segment]
	}
	return current
}

// isTable returns true if the value is a TOML table (map).
func isTable(v interface{}) bool {
	_, ok := v.(map[string]interface{})
	return ok
}

// YAMLToNative converts YAML data to TOML format using native libraries.
// If the YAML contains $comment$ map keys, they are converted back to TOML comments.
// Note: BurntSushi's TOML encoder sorts keys alphabetically; YAML source order is
// not preserved in the output.
func (*TOMLResourceProviderType) YAMLToNative(yamlData []byte) ([]byte, error) {
	if len(yamlData) == 0 {
		return []byte{}, nil
	}

	// Parse YAML with gaby to preserve key order in the data structure.
	// (BurntSushi's encoder will still sort alphabetically on output.)
	doc, err := gaby.ParseYAML(yamlData)
	if err != nil {
		return nil, fmt.Errorf("error parsing YAML: %w", err)
	}

	// Extract comment keys from data before TOML encoding.
	// Use Data() (unordered) since the TOML encoder sorts anyway.
	cleanData, comments := yamlkit.ExtractCommentsFromData(doc.Data())

	// Convert to TOML
	var output bytes.Buffer
	encoder := toml.NewEncoder(&output)
	if err := encoder.Encode(cleanData); err != nil {
		return nil, fmt.Errorf("error converting YAML to TOML: %w", err)
	}

	// Re-inject comments into TOML output
	result := injectTOMLComments(output.Bytes(), comments)

	return result, nil
}
