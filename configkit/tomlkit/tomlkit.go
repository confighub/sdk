// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

// Package tomlkit is used to interpret AppConfig/TOML configuration units.
package tomlkit

import (
	"bytes"
	"fmt"

	"github.com/BurntSushi/toml"
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

type TOMLResourceProviderType struct {
	yamlkit.ResourceProviderRegistry
}

// NewTOMLResourceProvider creates a new TOMLResourceProviderType with its own path registry.
func NewTOMLResourceProvider() *TOMLResourceProviderType {
	return &TOMLResourceProviderType{
		ResourceProviderRegistry: yamlkit.NewResourceProviderRegistry(),
	}
}

func (*TOMLResourceProviderType) MergeKeyForPath(_ api.ResourceType, _ string) (string, bool) {
	return "", false
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

// Deprecated: Use ResourceMergeIDGetter instead.
func (rp *TOMLResourceProviderType) ResourceIDGetter(doc *gaby.YamlDoc) (string, error) {
	resourceIDPath := rp.ContextPath(constants.ResourceIDKeySuffix)
	id, found, err := yamlkit.YamlSafePathGetValue[string](doc, api.ResolvedPath(resourceIDPath), true)
	if err != nil || !found {
		return "", err
	}
	return id, nil
}

// Deprecated: Use SetResourceMergeID instead.
func (rp *TOMLResourceProviderType) SetResourceID(doc *gaby.YamlDoc, id string) error {
	resourceIDPath := rp.ContextPath(constants.ResourceIDKeySuffix)
	_, err := doc.SetP(id, resourceIDPath)
	return err
}

// Deprecated: Use DeleteResourceMergeID instead.
func (rp *TOMLResourceProviderType) DeleteResourceID(doc *gaby.YamlDoc) error {
	resourceIDPath := rp.ContextPath(constants.ResourceIDKeySuffix)
	return doc.DeleteP(resourceIDPath)
}

func (rp *TOMLResourceProviderType) ResourceMergeIDGetter(doc *gaby.YamlDoc) (string, error) {
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

func (rp *TOMLResourceProviderType) SetResourceMergeID(doc *gaby.YamlDoc, id string) error {
	resourceMergeIDPath := rp.ContextPath(constants.ResourceMergeIDKeySuffix)
	_, err := doc.SetP(id, resourceMergeIDPath)
	return err
}

func (rp *TOMLResourceProviderType) DeleteResourceMergeID(doc *gaby.YamlDoc) error {
	resourceMergeIDPath := rp.ContextPath(constants.ResourceMergeIDKeySuffix)
	_ = doc.DeleteP(resourceMergeIDPath)
	// Also delete legacy ResourceID path.
	return rp.DeleteResourceID(doc)
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
func (*TOMLResourceProviderType) NativeToYAML(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return []byte{}, nil
	}

	// Parse TOML into a generic map
	var tomlData interface{}
	if err := toml.Unmarshal(data, &tomlData); err != nil {
		return nil, fmt.Errorf("error parsing TOML: %w", err)
	}

	// Convert to YAML
	var output bytes.Buffer
	encoder := yaml.NewEncoder(&output)
	encoder.SetIndent(2)
	if err := encoder.Encode(tomlData); err != nil {
		return nil, fmt.Errorf("error converting TOML to YAML: %w", err)
	}
	if err := encoder.Close(); err != nil {
		return nil, fmt.Errorf("error finalizing YAML encoding: %w", err)
	}

	return output.Bytes(), nil
}

// YAMLToNative converts YAML data to TOML format using native libraries.
func (*TOMLResourceProviderType) YAMLToNative(yamlData []byte) ([]byte, error) {
	if len(yamlData) == 0 {
		return []byte{}, nil
	}

	// Parse YAML into a generic map
	var data interface{}
	if err := yaml.Unmarshal(yamlData, &data); err != nil {
		return nil, fmt.Errorf("error parsing YAML: %w", err)
	}

	// Convert to TOML
	var output bytes.Buffer
	encoder := toml.NewEncoder(&output)
	if err := encoder.Encode(data); err != nil {
		return nil, fmt.Errorf("error converting YAML to TOML: %w", err)
	}

	return output.Bytes(), nil
}
