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
	"gopkg.in/yaml.v3"
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

func (*JSONResourceProviderType) MergeKeyForPath(_ api.ResourceType, _ string) (string, bool) {
	return "", false
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

// Deprecated: Use ResourceMergeIDGetter instead.
func (rp *JSONResourceProviderType) ResourceIDGetter(doc *gaby.YamlDoc) (string, error) {
	resourceIDPath := rp.ContextPath(constants.ResourceIDKeySuffix)
	id, found, err := yamlkit.YamlSafePathGetValue[string](doc, api.ResolvedPath(resourceIDPath), true)
	if err != nil || !found {
		return "", err
	}
	return id, nil
}

// Deprecated: Use SetResourceMergeID instead.
func (rp *JSONResourceProviderType) SetResourceID(doc *gaby.YamlDoc, id string) error {
	resourceIDPath := rp.ContextPath(constants.ResourceIDKeySuffix)
	_, err := doc.SetP(id, resourceIDPath)
	return err
}

// Deprecated: Use DeleteResourceMergeID instead.
func (rp *JSONResourceProviderType) DeleteResourceID(doc *gaby.YamlDoc) error {
	resourceIDPath := rp.ContextPath(constants.ResourceIDKeySuffix)
	return doc.DeleteP(resourceIDPath)
}

func (rp *JSONResourceProviderType) ResourceNameStableCoreGetter(doc *gaby.YamlDoc) (api.ResourceName, error) {
	resourceNameStableCorePath := rp.ContextPath(constants.ResourceNameStableCoreKeySuffix)
	name, found, err := yamlkit.YamlSafePathGetValue[string](doc, api.ResolvedPath(resourceNameStableCorePath), true)
	if err != nil || !found {
		return "", err
	}
	return api.ResourceName(name), nil
}

func (rp *JSONResourceProviderType) ResourceMergeIDGetter(doc *gaby.YamlDoc) (string, error) {
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

func (rp *JSONResourceProviderType) SetResourceMergeID(doc *gaby.YamlDoc, id string) error {
	resourceMergeIDPath := rp.ContextPath(constants.ResourceMergeIDKeySuffix)
	_, err := doc.SetP(id, resourceMergeIDPath)
	return err
}

func (rp *JSONResourceProviderType) DeleteResourceMergeID(doc *gaby.YamlDoc) error {
	resourceMergeIDPath := rp.ContextPath(constants.ResourceMergeIDKeySuffix)
	_ = doc.DeleteP(resourceMergeIDPath)
	// Also delete legacy ResourceID path.
	return rp.DeleteResourceID(doc)
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
func (*JSONResourceProviderType) NativeToYAML(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return []byte{}, nil
	}

	// Parse JSON into a generic structure
	var jsonData interface{}
	if err := json.Unmarshal(data, &jsonData); err != nil {
		return nil, fmt.Errorf("error parsing JSON: %w", err)
	}

	// Convert to YAML
	var output bytes.Buffer
	encoder := yaml.NewEncoder(&output)
	encoder.SetIndent(2)
	if err := encoder.Encode(jsonData); err != nil {
		return nil, fmt.Errorf("error converting JSON to YAML: %w", err)
	}
	if err := encoder.Close(); err != nil {
		return nil, fmt.Errorf("error finalizing YAML encoding: %w", err)
	}

	return output.Bytes(), nil
}

// YAMLToNative converts YAML data to JSON format.
func (*JSONResourceProviderType) YAMLToNative(yamlData []byte) ([]byte, error) {
	if len(yamlData) == 0 {
		return []byte{}, nil
	}

	// Parse YAML into a generic structure
	var data interface{}
	if err := yaml.Unmarshal(yamlData, &data); err != nil {
		return nil, fmt.Errorf("error parsing YAML: %w", err)
	}

	// Convert to JSON with indentation
	output, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("error converting YAML to JSON: %w", err)
	}

	// Add trailing newline
	output = append(output, '\n')

	return output, nil
}
