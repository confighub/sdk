// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

// Package cubkit is used to interpret ConfigHub/YAML configuration units.
package cubkit

import (
	"github.com/confighub/sdk/configkit/yamlkit"
	"github.com/confighub/sdk/function/api"
	"github.com/confighub/sdk/third_party/gaby"
)

// User data errors should not be logged here. They will be logged by the caller.
// Errors indicate that the operation could not be completed.
// Messages should be acceptable to return to the user, and should indicate the
// location of the problem in the configuration data.

type ConfigHubResourceProviderType struct{}

var pathRegistry = make(api.AttributeNameToResourceTypeToPathToVisitorInfoType)

func (*ConfigHubResourceProviderType) GetPathRegistry() api.AttributeNameToResourceTypeToPathToVisitorInfoType {
	return pathRegistry
}

// ConfigHubResourceProvider implements the ResourceProvider interface for AppConfig/ConfigHub.
var ConfigHubResourceProvider = &ConfigHubResourceProviderType{}

// DefaultResourceCategory returns the default resource category to asssume, which is AppConfig in this case.
func (*ConfigHubResourceProviderType) DefaultResourceCategory() api.ResourceCategory {
	return api.ResourceCategoryResource
}

// ResourceCategoryGetter just returns ResourceCategoryAppConfig for ConfigHub documents.
func (*ConfigHubResourceProviderType) ResourceCategoryGetter(doc *gaby.YamlDoc) (api.ResourceCategory, error) {
	// TODO: check that the document is non-empty?
	return api.ResourceCategoryResource, nil
}

var ConfigHubNonSpaceScopedEntityTypes = map[api.ResourceType]struct{}{
	api.ResourceType("Organization"):       {},
	api.ResourceType("OrganizationMember"): {},
	api.ResourceType("User"):               {},
	api.ResourceType("Space"):              {},
}

const (
	ResourceTypeNoEntityType = api.ResourceType("NoEntityType")
	ResourceNameNoName       = api.ResourceName("NoName")
	EntityTypePath           = api.ResolvedPath("EntityType")
	EntityNamePath           = api.ResolvedPath("Slug")
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
	// TODO: SpaceSlug
	name, hasName, err := yamlkit.YamlSafePathGetValue[string](doc, EntityNamePath, true)
	if err != nil {
		return "", err
	}
	if hasName {
		return api.ResourceName(name), nil
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

func (*ConfigHubResourceProviderType) TypeDescription() string {
	return "EntityType"
}

const nameSeparatorString = "" // TODO: "/"

func (*ConfigHubResourceProviderType) NormalizeName(name string) string {
	// Virtually all characters are valid
	return name
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
func (*ConfigHubResourceProviderType) ResourceAndCategoryTypeMaps(docs gaby.Container) (resourceMap yamlkit.ResourceNameToCategoryTypesMap, categoryTypeMap yamlkit.ResourceCategoryTypeToNamesMap, err error) {
	return yamlkit.ResourceAndCategoryTypeMaps(docs, ConfigHubResourceProvider)
}

func (*ConfigHubResourceProviderType) RemoveScopeFromResourceName(resourceName api.ResourceName) api.ResourceName {
	return resourceName
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
