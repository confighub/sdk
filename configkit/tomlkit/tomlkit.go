// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

// Package tomlkit is used to interpret AppConfig/TOML configuration units.
package tomlkit

import (
	"fmt"
	"strings"

	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/constants"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/third_party/gaby"
	"github.com/confighub/sdk/core/workerapi"
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
		ResourceProviderRegistry: yamlkit.NewResourceProviderRegistry(workerapi.ToolchainAppConfigTOML),
	}
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

// RemoveAPIVersionFromResourceType removes the version from a configuration schema, such as
// v1alpha3 from registrybot.confighub.com/v1alpha3 (see yamlkit.RemoveSchemaVersionSegments).
func (*TOMLResourceProviderType) RemoveAPIVersionFromResourceType(resourceType api.ResourceType) api.ResourceType {
	return yamlkit.RemoveSchemaVersionSegments(resourceType)
}

func (rp *TOMLResourceProviderType) ResourceTypesAreSimilar(resourceTypeA, resourceTypeB api.ResourceType) bool {
	return api.SameResourceType(rp, resourceTypeA, resourceTypeB)
}

func (*TOMLResourceProviderType) DataType() api.DataType {
	return api.DataTypeTOML
}

func (*TOMLResourceProviderType) GetToolchainType() workerapi.ToolchainType {
	return workerapi.ToolchainAppConfigTOML
}

// NativeToYAML converts TOML data to YAML format.
// Comments from the TOML source are preserved as $comment$ map keys in the output.
// Key order from the TOML source is preserved, and numbers and date-times
// keep the text they are written as (see tomlToYAML).
func (*TOMLResourceProviderType) NativeToYAML(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return []byte{}, nil
	}
	doc, err := tomlToYAML(data)
	if err != nil {
		return nil, err
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

// YAMLToNative converts YAML data to TOML format.
// If the YAML contains $comment$ map keys, they are converted back to TOML comments.
// Key order from the YAML source is preserved, apart from the keys with
// plain values of each table coming before its sub-tables (see yamlToTOML).
func (*TOMLResourceProviderType) YAMLToNative(yamlData []byte) ([]byte, error) {
	if len(yamlData) == 0 {
		return []byte{}, nil
	}
	doc, err := gaby.ParseYAML(yamlData)
	if err != nil {
		return nil, fmt.Errorf("error parsing YAML: %w", err)
	}
	return yamlToTOML(doc)
}
