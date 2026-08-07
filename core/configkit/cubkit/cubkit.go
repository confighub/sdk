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

func (*ConfigHubResourceProviderType) MergeKeysForPath(_ api.ResourceType, _ string) ([]string, bool) {
	return nil, false
}

// ExclusiveFieldsForPath returns no union: this format has no schema declaring one.
func (*ConfigHubResourceProviderType) ExclusiveFieldsForPath(_ api.ResourceType, _ string) (yamlkit.ExclusiveFields, bool) {
	return yamlkit.ExclusiveFields{}, false
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

func (rp *ConfigHubResourceProviderType) ResourceNameStableCoreGetter(doc *gaby.YamlDoc) (api.ResourceName, error) {
	resourceNameStableCorePath := rp.ContextPath(constants.ResourceNameStableCoreKeySuffix)
	name, found, err := yamlkit.YamlSafePathGetValue[string](doc, api.ResolvedPath(resourceNameStableCorePath), true)
	if err != nil || !found {
		return "", err
	}
	return api.ResourceName(name), nil
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

// NativeToYAML converts native YAML comments (HeadComment, LineComment, FootComment)
// into $comment$ map keys so they have a uniform representation across all formats.
func (*ConfigHubResourceProviderType) NativeToYAML(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return data, nil
	}
	docs, err := gaby.ParseAll(data)
	if err != nil {
		return data, nil
	}
	for _, doc := range docs {
		doc.ExtractCommentsToKeys()
	}
	return docs.Bytes(), nil
}

// YAMLToNative converts $comment$ map keys back into native YAML comments.
func (*ConfigHubResourceProviderType) YAMLToNative(yamlData []byte) ([]byte, error) {
	if len(yamlData) == 0 {
		return yamlData, nil
	}
	docs, err := gaby.ParseAll(yamlData)
	if err != nil {
		return yamlData, nil
	}
	for _, doc := range docs {
		doc.InjectCommentsFromKeys()
	}
	return docs.Bytes(), nil
}

func (*ConfigHubResourceProviderType) DataType() api.DataType {
	return api.DataTypeYAML
}

func (*ConfigHubResourceProviderType) GetToolchainType() workerapi.ToolchainType {
	return workerapi.ToolchainConfigHubYAML
}
