// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

// Package jsonkit is used to interpret AppConfig/JSON configuration units.
package jsonkit

import (
	"bytes"
	"encoding/json"
	"fmt"

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

type JSONResourceProviderType struct {
	yamlkit.ResourceProviderRegistry
}

// NewJSONResourceProvider creates a new JSONResourceProviderType with its own path registry.
func NewJSONResourceProvider() *JSONResourceProviderType {
	return &JSONResourceProviderType{
		ResourceProviderRegistry: yamlkit.NewResourceProviderRegistry(),
	}
}

func (*JSONResourceProviderType) MergeKeysForPath(_ api.ResourceType, _ string) ([]string, bool) {
	return nil, false
}

func (*JSONResourceProviderType) IsMapKeyPath(_ api.ResourceType, _ string) bool {
	return false
}

// DefaultResourceCategory returns the default resource category to assume, which is AppConfig in this case.
func (*JSONResourceProviderType) DefaultResourceCategory() api.ResourceCategory {
	return api.ResourceCategoryAppConfig
}

// ResourceCategoryGetter just returns ResourceCategoryAppConfig for JSON documents.
func (*JSONResourceProviderType) ResourceCategoryGetter(doc *gaby.YamlDoc) (api.ResourceCategory, error) {
	return api.ResourceCategoryAppConfig, nil
}

const (
	ResourceTypeNoSchema = api.ResourceType("NoSchema")
	ResourceNameNoName   = api.ResourceName("NoName")
	ConfigSchemaPath     = api.ResolvedPath("configHub.configSchema")
	ConfigNamePath       = api.ResolvedPath("configHub.configName")
)

// ResourceTypeGetter extracts the property configHub.configSchema, and returns NoSchema if not present.
func (*JSONResourceProviderType) ResourceTypeGetter(doc *gaby.YamlDoc) (api.ResourceType, error) {
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
func (*JSONResourceProviderType) ResourceNameGetter(doc *gaby.YamlDoc) (api.ResourceName, error) {
	name, hasName, err := yamlkit.YamlSafePathGetValue[string](doc, ConfigNamePath, true)
	if err != nil {
		return "", err
	}
	if hasName {
		return api.ResourceName(name), nil
	}
	return ResourceNameNoName, nil
}

func (*JSONResourceProviderType) ScopelessResourceNamePath() api.ResolvedPath {
	return ConfigNamePath
}

func (*JSONResourceProviderType) SetResourceName(doc *gaby.YamlDoc, name string) error {
	_, err := doc.SetP(name, string(ConfigNamePath))
	return err
}

func (rp *JSONResourceProviderType) ResourceNameStableCoreGetter(doc *gaby.YamlDoc) (api.ResourceName, error) {
	resourceNameStableCorePath := rp.ContextPath(constants.ResourceNameStableCoreKeySuffix)
	name, found, err := yamlkit.YamlSafePathGetValue[string](doc, api.ResolvedPath(resourceNameStableCorePath), true)
	if err != nil || !found {
		return "", err
	}
	return api.ResourceName(name), nil
}

func (*JSONResourceProviderType) TypeDescription() string {
	return "Schema"
}

const nameSeparatorString = ""

func (*JSONResourceProviderType) NormalizeName(name string) string {
	return name
}

func (*JSONResourceProviderType) NameSeparator() string {
	return nameSeparatorString
}

const (
	contextPathPrefx = "configHub."
)

func (*JSONResourceProviderType) ContextPath(contextField string) string {
	return contextPathPrefx + yamlkit.LowerFirst(contextField)
}

// ResourceAndCategoryTypeMaps returns maps of all resources in the provided list of parsed YAML
// documents, from from names to categories+types and categories+types to names.
func (rp *JSONResourceProviderType) ResourceAndCategoryTypeMaps(docs gaby.Container) (resourceMap yamlkit.ResourceNameToCategoryTypesMap, categoryTypeMap yamlkit.ResourceCategoryTypeToNamesMap, err error) {
	return yamlkit.ResourceAndCategoryTypeMaps(docs, rp)
}

func (*JSONResourceProviderType) RemoveScopeFromResourceName(resourceName api.ResourceName) api.ResourceName {
	return resourceName
}

func (*JSONResourceProviderType) ResourceTypesAreSimilar(resourceTypeA, resourceTypeB api.ResourceType) bool {
	return resourceTypeA == resourceTypeB
}

func (*JSONResourceProviderType) DataType() api.DataType {
	return api.DataTypeJSON
}

func (*JSONResourceProviderType) GetToolchainType() workerapi.ToolchainType {
	return workerapi.ToolchainAppConfigJSON
}

// NativeToYAML converts JSON data to YAML format.
// Key order from the JSON source is preserved.
func (*JSONResourceProviderType) NativeToYAML(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return []byte{}, nil
	}

	// Parse JSON with gaby (preserves key order via kyaml)
	doc, err := gaby.ParseJSON(data)
	if err != nil {
		return nil, fmt.Errorf("error parsing JSON: %w", err)
	}

	return doc.Bytes(), nil
}

// YAMLToNative converts YAML data to JSON format.
func (*JSONResourceProviderType) YAMLToNative(yamlData []byte) ([]byte, error) {
	if len(yamlData) == 0 {
		return []byte{}, nil
	}

	// Parse YAML with gaby
	doc, err := gaby.ParseYAML(yamlData)
	if err != nil {
		return nil, fmt.Errorf("error parsing YAML: %w", err)
	}

	// Get compact JSON from gaby (preserves key order)
	compactJSON, err := doc.MarshalJSON()
	if err != nil {
		return nil, fmt.Errorf("error converting YAML to JSON: %w", err)
	}

	// Re-indent for readability (json.Indent preserves key order)
	var indented bytes.Buffer
	if err := json.Indent(&indented, compactJSON, "", "  "); err != nil {
		return nil, fmt.Errorf("error indenting JSON: %w", err)
	}
	indented.WriteByte('\n')

	return indented.Bytes(), nil
}
