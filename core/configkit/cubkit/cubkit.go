// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

// Package cubkit is used to interpret ConfigHub/YAML configuration units.
package cubkit

import (
	"regexp"
	"strings"

	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/constants"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/workerapi"
	"github.com/confighub/sdk/core/third_party/gaby"
	"github.com/google/uuid"
)

// User data errors should not be logged here. They will be logged by the caller.
// Errors indicate that the operation could not be completed.
// Messages should be acceptable to return to the user, and should indicate the
// location of the problem in the configuration data.

type ConfigHubResourceProviderType struct {
	yamlkit.ResourceProviderRegistry
}

// NewConfigHubResourceProvider creates a new ConfigHubResourceProviderType with its own path registry.
func NewConfigHubResourceProvider() *ConfigHubResourceProviderType {
	return &ConfigHubResourceProviderType{
		ResourceProviderRegistry: yamlkit.NewResourceProviderRegistry(),
	}
}

func (*ConfigHubResourceProviderType) MergeKeyForPath(_ api.ResourceType, _ string) (string, bool) {
	return "", false
}

func (*ConfigHubResourceProviderType) IsMapKeyPath(_ api.ResourceType, _ string) bool {
	return false
}

// DefaultResourceCategory returns the default resource category to asssume, which is AppConfig in this case.
func (*ConfigHubResourceProviderType) DefaultResourceCategory() api.ResourceCategory {
	return api.ResourceCategoryResource
}

// ResourceCategoryGetter just returns ResourceCategoryResource for ConfigHub/YAML documents.
func (*ConfigHubResourceProviderType) ResourceCategoryGetter(doc *gaby.YamlDoc) (api.ResourceCategory, error) {
	// TODO: check that the document is non-empty?
	return api.ResourceCategoryResource, nil
}

var ConfigHubNonSpaceScopedEntityTypes = map[api.ResourceType]bool{
	api.ResourceType("Organization"):       true,
	api.ResourceType("OrganizationMember"): true,
	api.ResourceType("User"):               true,
	api.ResourceType("Space"):              true,
}

const (
	ResourceTypeNoEntityType = api.ResourceType("NoEntityType")
	ResourceNameNoName       = api.ResourceName("NoName")
	EntityTypePath           = api.ResolvedPath("EntityType")
	EntityNamePath           = api.ResolvedPath("Slug")
	EntityScopePath          = api.ResolvedPath("SpaceSlug")
)

// ResourceTypeGetter extracts the property EntityType, and returns NoEntityType if not present.
func (*ConfigHubResourceProviderType) ResourceTypeGetter(doc *gaby.YamlDoc) (api.ResourceType, error) {
	entityType, hasEntityType, err := yamlkit.YamlSafePathGetValue[string](doc, EntityTypePath, true)
	if err != nil {
		return "", err
	}
	if hasEntityType {
		return api.ResourceType(entityType), nil
	}
	return ResourceTypeNoEntityType, nil
}

// ResourceNameGetter extracts the property Slug, and returns NoName if not present.
func (*ConfigHubResourceProviderType) ResourceNameGetter(doc *gaby.YamlDoc) (api.ResourceName, error) {
	name, hasName, err := yamlkit.YamlSafePathGetValue[string](doc, EntityNamePath, true)
	if err != nil {
		return "", err
	}
	spaceName, _, err := yamlkit.YamlSafePathGetValue[string](doc, EntityScopePath, true)
	if err != nil {
		return "", err
	}
	if hasName {
		return api.ResourceName(spaceName + "/" + name), nil
	}
	return ResourceNameNoName, nil
}

func (*ConfigHubResourceProviderType) ScopelessResourceNamePath() api.ResolvedPath {
	return EntityNamePath
}

func (*ConfigHubResourceProviderType) SetResourceName(doc *gaby.YamlDoc, name string) error {
	_, err := doc.SetP(name, string(EntityNamePath))
	return err
}

// Deprecated: Use ResourceMergeIDGetter instead.
func (rp *ConfigHubResourceProviderType) ResourceIDGetter(doc *gaby.YamlDoc) (string, error) {
	resourceIDPath := rp.ContextPath(constants.ResourceIDKeySuffix)
	id, found, err := yamlkit.YamlSafePathGetValue[string](doc, api.ResolvedPath(resourceIDPath), true)
	if err != nil || !found {
		return "", err
	}
	return id, nil
}

// Deprecated: Use SetResourceMergeID instead.
func (rp *ConfigHubResourceProviderType) SetResourceID(doc *gaby.YamlDoc, id string) error {
	resourceIDPath := rp.ContextPath(constants.ResourceIDKeySuffix)
	_, err := doc.SetP(id, resourceIDPath)
	return err
}

// Deprecated: Use DeleteResourceMergeID instead.
func (rp *ConfigHubResourceProviderType) DeleteResourceID(doc *gaby.YamlDoc) error {
	resourceIDPath := rp.ContextPath(constants.ResourceIDKeySuffix)
	return doc.DeleteP(resourceIDPath)
}

func (rp *ConfigHubResourceProviderType) ResourceNameStableCoreGetter(doc *gaby.YamlDoc) (api.ResourceName, error) {
	resourceNameStableCorePath := rp.ContextPath(constants.ResourceNameStableCoreKeySuffix)
	name, found, err := yamlkit.YamlSafePathGetValue[string](doc, api.ResolvedPath(resourceNameStableCorePath), true)
	if err != nil || !found {
		return "", err
	}
	return api.ResourceName(name), nil
}

func (rp *ConfigHubResourceProviderType) ResourceMergeIDGetter(doc *gaby.YamlDoc) (string, error) {
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

func (rp *ConfigHubResourceProviderType) SetResourceMergeID(doc *gaby.YamlDoc, id string) error {
	resourceMergeIDPath := rp.ContextPath(constants.ResourceMergeIDKeySuffix)
	_, err := doc.SetP(id, resourceMergeIDPath)
	return err
}

func (rp *ConfigHubResourceProviderType) DeleteResourceMergeID(doc *gaby.YamlDoc) error {
	resourceMergeIDPath := rp.ContextPath(constants.ResourceMergeIDKeySuffix)
	_ = doc.DeleteP(resourceMergeIDPath)
	// Also delete legacy ResourceID path.
	return rp.DeleteResourceID(doc)
}

func (*ConfigHubResourceProviderType) TypeDescription() string {
	return "EntityType"
}

const nameSeparatorString = "/"

const slugCoreChars string = "\\-_.A-Za-z0-9"
const slugInvalidRegexpString string = "[^" + slugCoreChars + "]"

var slugInvalidRegexp = regexp.MustCompile(slugInvalidRegexpString)

const multipleDashesString = "---*"

var multipleDashesRegexp = regexp.MustCompile(multipleDashesString)

func CubNormalizeName(providedText string) string {
	// Note: Not lowercased and not converted to kabob-case
	// Also not internationalized.

	// Remove leading and trailing spaces
	slug := strings.TrimSpace(providedText)
	// Remove invalid characters
	slug = slugInvalidRegexp.ReplaceAllString(slug, "-")
	// Collapse multiple consecutive dashes. There could be consecutive punctuation.
	slug = multipleDashesRegexp.ReplaceAllString(slug, "-")
	// Trim leading and trailing punctuation
	slug = strings.Trim(slug, "-._")
	// Don't allow UUIDs
	_, err := uuid.Parse(slug)
	if err == nil {
		slug = "id" + slug
	}
	return slug
}

func (*ConfigHubResourceProviderType) NormalizeName(name string) string {
	return CubNormalizeName(name)
}

func (*ConfigHubResourceProviderType) NameSeparator() string {
	return nameSeparatorString
}

const (
	contextKeyPrefix = "confighub.com/"
	contextPathPrefx = "Annotations."
)

func (*ConfigHubResourceProviderType) ContextPath(contextField string) string {
	// PascalCase is expected for contextField
	safeKey := yamlkit.EscapeDotsInPathSegment(contextKeyPrefix + contextField)
	return contextPathPrefx + safeKey
}

// ResourceAndCategoryTypeMaps returns maps of all resources in the provided list of parsed YAML
// documents, from from names to categories+types and categories+types to names.
func (rp *ConfigHubResourceProviderType) ResourceAndCategoryTypeMaps(docs gaby.Container) (resourceMap yamlkit.ResourceNameToCategoryTypesMap, categoryTypeMap yamlkit.ResourceCategoryTypeToNamesMap, err error) {
	return yamlkit.ResourceAndCategoryTypeMaps(docs, rp)
}

func (c *ConfigHubResourceProviderType) RemoveScopeFromResourceName(resourceName api.ResourceName) api.ResourceName {
	_, justResourceName := c.ParseResourceName(resourceName)
	return api.ResourceName(justResourceName)
}

func (*ConfigHubResourceProviderType) ParseResourceName(resourceName api.ResourceName) (string, string) {
	spaceSlug, unscopedResourceName, found := strings.Cut(string(resourceName), "/")
	if !found {
		unscopedResourceName = spaceSlug
		spaceSlug = ""
	}
	return spaceSlug, unscopedResourceName
}

// TODO: Trigger and Invocation?
func (*ConfigHubResourceProviderType) ResourceTypesAreSimilar(resourceTypeA, resourceTypeB api.ResourceType) bool {
	return resourceTypeA == resourceTypeB
}

// The conversions are no-ops since ConfigHub/YAML is already YAML.

func (*ConfigHubResourceProviderType) NativeToYAML(data []byte) ([]byte, error) {
	// TODO: deep copy?
	return data, nil
}

func (*ConfigHubResourceProviderType) YAMLToNative(yamlData []byte) ([]byte, error) {
	// TODO: deep copy?
	return yamlData, nil
}

func (*ConfigHubResourceProviderType) DataType() api.DataType {
	return api.DataTypeYAML
}

func (*ConfigHubResourceProviderType) GetToolchainType() workerapi.ToolchainType {
	return workerapi.ToolchainConfigHubYAML
}
