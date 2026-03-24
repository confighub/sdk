// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

// Package hclkit is used to interpret OpenTofu/HCL configuration units.
package hclkit

import (
	"errors"
	"strings"

	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/constants"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/workerapi"
	"github.com/confighub/sdk/core/third_party/gaby"
	"github.com/gosimple/slug"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

// User data errors should not be logged here. They will be logged by the caller.
// Errors indicate that the operation could not be completed.
// Messages should be acceptable to return to the user, and should indicate the
// location of the problem in the configuration data.

type HclResourceProviderType struct {
	pathRegistry      api.AttributeNameToResourceTypeToPathToVisitorInfoType
	attributeRegistry api.AttributeNameToAttributeDescriptor
}

// NewHclResourceProvider creates a new HclResourceProviderType with its own path registry.
func NewHclResourceProvider() *HclResourceProviderType {
	return &HclResourceProviderType{
		pathRegistry:      make(api.AttributeNameToResourceTypeToPathToVisitorInfoType),
		attributeRegistry: make(api.AttributeNameToAttributeDescriptor),
	}
}

func (*HclResourceProviderType) MergeKeyForPath(_ api.ResourceType, _ string) (string, bool) {
	return "", false
}

func (rp *HclResourceProviderType) GetPathRegistry() api.AttributeNameToResourceTypeToPathToVisitorInfoType {
	return rp.pathRegistry
}

func (rp *HclResourceProviderType) GetAttributeRegistry() api.AttributeNameToAttributeDescriptor {
	return rp.attributeRegistry
}

// Block metadata are translated to properties at known paths, unlike tfjson's nested
// blocks, because that also enables resource/data paths to be at fixed locations rather
// than at paths parameterized by resource name.

const (
	MetadataPrefix     = "confighub"
	BlockCategoryField = "block_category"
	BlockTypeField     = "block_type"
	BlockNameField     = "block_name"
	BlockNameSingleton = "singleton"
	BlockCategoryPath  = api.ResolvedPath(MetadataPrefix + "." + BlockCategoryField)
	BlockTypePath      = api.ResolvedPath(MetadataPrefix + "." + BlockTypeField)
	BlockNamePath      = api.ResolvedPath(MetadataPrefix + "." + BlockNameField)
)

var blockTypeToCategory = map[string]api.ResourceCategory{
	"resource": api.ResourceCategoryResource,
	"data":     api.ResourceCategoryDyanmicData,
}

var categoryToBlockType = map[api.ResourceCategory]string{
	api.ResourceCategoryResource:    "resource",
	api.ResourceCategoryDyanmicData: "data",
}

func convertBlockTypeToCategory(bt string) api.ResourceCategory {
	category, found := blockTypeToCategory[bt]
	if found {
		return category
	}
	return api.ResourceCategoryInvalid
}

func convertCategoryToBlockType(category api.ResourceCategory) string {
	bt, found := categoryToBlockType[category]
	if found {
		return bt
	}
	return "invalid"

}

// ResourceCategoryGetter just returns ResourceCategoryResource for Kubernetes documents.
func (*HclResourceProviderType) ResourceCategoryGetter(doc *gaby.YamlDoc) (api.ResourceCategory, error) {
	// TODO: check that the document is non-empty?
	category, hasCategory, err := yamlkit.YamlSafePathGetValue[string](doc, BlockCategoryPath, true)
	if err != nil {
		return "", err
	}
	if hasCategory {
		return api.ResourceCategory(category), nil
	}
	return "", errors.New("no resource category found")
}

// DefaultResourceCategory returns the default resource category to asssume, which is Resource in this case.
func (*HclResourceProviderType) DefaultResourceCategory() api.ResourceCategory {
	return api.ResourceCategoryResource
}

// ResourceTypeGetter extracts the property configHub.configSchema, and returns NoSchema if not present.
func (*HclResourceProviderType) ResourceTypeGetter(doc *gaby.YamlDoc) (api.ResourceType, error) {
	resourceType, hasType, err := yamlkit.YamlSafePathGetValue[string](doc, BlockTypePath, true)
	if err != nil {
		return "", err
	}
	if hasType {
		return api.ResourceType(resourceType), nil
	}
	return "", errors.New("no resource type found")
}

// ResourceNameGetter extracts the property configHub.configName, and returns NoName if not present.
func (*HclResourceProviderType) ResourceNameGetter(doc *gaby.YamlDoc) (api.ResourceName, error) {
	name, hasName, err := yamlkit.YamlSafePathGetValue[string](doc, BlockNamePath, true)
	if err != nil {
		return "", err
	}
	if hasName {
		return api.ResourceName(name), nil
	}
	return "", errors.New("no resource name found")
}

func (*HclResourceProviderType) ScopelessResourceNamePath() api.ResolvedPath {
	return BlockNamePath
}

func (*HclResourceProviderType) SetResourceName(doc *gaby.YamlDoc, name string) error {
	_, err := doc.SetP(name, string(BlockNamePath))
	return err
}

func (rp *HclResourceProviderType) ResourceIDGetter(doc *gaby.YamlDoc) (string, error) {
	resourceIDPath := rp.ContextPath(constants.ResourceIDKeySuffix)
	id, found, err := yamlkit.YamlSafePathGetValue[string](doc, api.ResolvedPath(resourceIDPath), true)
	if err != nil || !found {
		return "", err
	}
	return id, nil
}

func (rp *HclResourceProviderType) SetResourceID(doc *gaby.YamlDoc, id string) error {
	resourceIDPath := rp.ContextPath(constants.ResourceIDKeySuffix)
	_, err := doc.SetP(id, resourceIDPath)
	return err
}

func (rp *HclResourceProviderType) DeleteResourceID(doc *gaby.YamlDoc) error {
	resourceIDPath := rp.ContextPath(constants.ResourceIDKeySuffix)
	return doc.DeleteP(resourceIDPath)
}

const nameSeparatorString = "_"

func (*HclResourceProviderType) NormalizeName(name string) string {
	s := cases.Title(language.Und, cases.NoLower).String(name)
	s = strings.ReplaceAll(s, "_", nameSeparatorString)
	s = strings.ToLower(slug.Make(s))
	return s
}

func (*HclResourceProviderType) NameSeparator() string {
	return nameSeparatorString
}

// TODO: is there a way to add extended attributes in OpenTofu?
func (*HclResourceProviderType) ContextPath(contextField string) string {
	return ""
}

// ResourceAndCategoryTypeMaps returns maps of all resources in the provided list of parsed YAML
// documents, from from names to categories+types and categories+types to names.
func (rp *HclResourceProviderType) ResourceAndCategoryTypeMaps(docs gaby.Container) (resourceMap yamlkit.ResourceNameToCategoryTypesMap, categoryTypeMap yamlkit.ResourceCategoryTypeToNamesMap, err error) {
	return yamlkit.ResourceAndCategoryTypeMaps(docs, rp)
}

func (*HclResourceProviderType) RemoveScopeFromResourceName(resourceName api.ResourceName) api.ResourceName {
	return resourceName
}

func (*HclResourceProviderType) TypeDescription() string {
	return "First HCL Label"
}

func (*HclResourceProviderType) ResourceTypesAreSimilar(resourceTypeA, resourceTypeB api.ResourceType) bool {
	return resourceTypeA == resourceTypeB
}

func (*HclResourceProviderType) DataType() api.DataType {
	return api.DataTypeHCL
}

func (*HclResourceProviderType) GetToolchainType() workerapi.ToolchainType {
	return workerapi.ToolchainOpenTofuHCL
}
