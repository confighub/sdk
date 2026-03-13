// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

// Package propkit is used to interpret AppConfig/Properties configuration units.
package propkit

import (
	"github.com/confighub/sdk/configkit/yamlkit"
	"github.com/confighub/sdk/constants"
	"github.com/confighub/sdk/function/api"
	"github.com/confighub/sdk/workerapi"
	"github.com/confighub/sdk/third_party/gaby"
)

// User data errors should not be logged here. They will be logged by the caller.
// Errors indicate that the operation could not be completed.
// Messages should be acceptable to return to the user, and should indicate the
// location of the problem in the configuration data.

type PropertiesResourceProviderType struct {
	pathRegistry      api.AttributeNameToResourceTypeToPathToVisitorInfoType
	attributeRegistry api.AttributeNameToAttributeDescriptor
}

// NewPropertiesResourceProvider creates a new PropertiesResourceProviderType with its own path registry.
func NewPropertiesResourceProvider() *PropertiesResourceProviderType {
	return &PropertiesResourceProviderType{
		pathRegistry:      make(api.AttributeNameToResourceTypeToPathToVisitorInfoType),
		attributeRegistry: make(api.AttributeNameToAttributeDescriptor),
	}
}

func (rp *PropertiesResourceProviderType) GetPathRegistry() api.AttributeNameToResourceTypeToPathToVisitorInfoType {
	return rp.pathRegistry
}

func (rp *PropertiesResourceProviderType) GetAttributeRegistry() api.AttributeNameToAttributeDescriptor {
	return rp.attributeRegistry
}

func (*PropertiesResourceProviderType) MergeKeyForPath(_ api.ResourceType, _ string) (string, bool) {
	return "", false
}

// DefaultResourceCategory returns the default resource category to asssume, which is AppConfig in this case.
func (*PropertiesResourceProviderType) DefaultResourceCategory() api.ResourceCategory {
	return api.ResourceCategoryAppConfig
}

// ResourceCategoryGetter just returns ResourceCategoryAppConfig for Properties documents.
func (*PropertiesResourceProviderType) ResourceCategoryGetter(doc *gaby.YamlDoc) (api.ResourceCategory, error) {
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
func (*PropertiesResourceProviderType) ResourceTypeGetter(doc *gaby.YamlDoc) (api.ResourceType, error) {
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
func (*PropertiesResourceProviderType) ResourceNameGetter(doc *gaby.YamlDoc) (api.ResourceName, error) {
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

func (*PropertiesResourceProviderType) ScopelessResourceNamePath() api.ResolvedPath {
	return ConfigNamePath
}

func (*PropertiesResourceProviderType) SetResourceName(doc *gaby.YamlDoc, name string) error {
	_, err := doc.SetP(name, string(ConfigNamePath))
	return err
}

func (rp *PropertiesResourceProviderType) ResourceIDGetter(doc *gaby.YamlDoc) (string, error) {
	resourceIDPath := rp.ContextPath(constants.ResourceIDKeySuffix)
	id, found, err := yamlkit.YamlSafePathGetValue[string](doc, api.ResolvedPath(resourceIDPath), true)
	if err != nil || !found {
		return "", err
	}
	return id, nil
}

func (rp *PropertiesResourceProviderType) SetResourceID(doc *gaby.YamlDoc, id string) error {
	resourceIDPath := rp.ContextPath(constants.ResourceIDKeySuffix)
	_, err := doc.SetP(id, resourceIDPath)
	return err
}

func (*PropertiesResourceProviderType) TypeDescription() string {
	return "Schema"
}

const nameSeparatorString = ""

func (*PropertiesResourceProviderType) NormalizeName(name string) string {
	// Virtually all characters are valid
	return name
}

func (*PropertiesResourceProviderType) NameSeparator() string {
	return nameSeparatorString
}

const (
	contextPathPrefx = "configHub."
)

func (*PropertiesResourceProviderType) ContextPath(contextField string) string {
	return contextPathPrefx + yamlkit.LowerFirst(contextField)
}

// ResourceAndCategoryTypeMaps returns maps of all resources in the provided list of parsed YAML
// documents, from from names to categories+types and categories+types to names.
func (rp *PropertiesResourceProviderType) ResourceAndCategoryTypeMaps(docs gaby.Container) (resourceMap yamlkit.ResourceNameToCategoryTypesMap, categoryTypeMap yamlkit.ResourceCategoryTypeToNamesMap, err error) {
	return yamlkit.ResourceAndCategoryTypeMaps(docs, rp)
}

func (*PropertiesResourceProviderType) RemoveScopeFromResourceName(resourceName api.ResourceName) api.ResourceName {
	return resourceName
}

func (*PropertiesResourceProviderType) ResourceTypesAreSimilar(resourceTypeA, resourceTypeB api.ResourceType) bool {
	return resourceTypeA == resourceTypeB
}

func (*PropertiesResourceProviderType) DataType() api.DataType {
	return api.DataTypeProperties
}

func (*PropertiesResourceProviderType) GetToolchainType() workerapi.ToolchainType {
	return workerapi.ToolchainAppConfigProperties
}
