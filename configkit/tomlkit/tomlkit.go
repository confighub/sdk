// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

// Package tomlkit is used to interpret AppConfig/TOML configuration units.
package tomlkit

import (
	"bytes"
	"fmt"

	"github.com/BurntSushi/toml"
	"github.com/confighub/sdk/configkit/yamlkit"
	"github.com/confighub/sdk/function/api"
	"github.com/confighub/sdk/third_party/gaby"
	"gopkg.in/yaml.v3"
)

// User data errors should not be logged here. They will be logged by the caller.
// Errors indicate that the operation could not be completed.
// Messages should be acceptable to return to the user, and should indicate the
// location of the problem in the configuration data.

type TOMLResourceProviderType struct{}

var pathRegistry = make(api.AttributeNameToResourceTypeToPathToVisitorInfoType)

func (*TOMLResourceProviderType) GetPathRegistry() api.AttributeNameToResourceTypeToPathToVisitorInfoType {
	return pathRegistry
}

// TOMLResourceProvider implements the ResourceProvider interface for AppConfig/TOML.
var TOMLResourceProvider = &TOMLResourceProviderType{}

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
func (*TOMLResourceProviderType) ResourceAndCategoryTypeMaps(docs gaby.Container) (resourceMap yamlkit.ResourceNameToCategoryTypesMap, categoryTypeMap yamlkit.ResourceCategoryTypeToNamesMap, err error) {
	return yamlkit.ResourceAndCategoryTypeMaps(docs, TOMLResourceProvider)
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
