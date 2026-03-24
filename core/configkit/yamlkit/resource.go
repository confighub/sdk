// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package yamlkit

import (
	"github.com/cockroachdb/errors"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/third_party/gaby"
	"github.com/confighub/sdk/core/workerapi"
)

// This is not in a more general place because it is expected to be used after conversion of other
// formats to YAML.

// The ResourceProvider interface is used to perform toolchain-specific operations.
type ResourceProvider interface {
	DefaultResourceCategory() api.ResourceCategory
	ResourceCategoryGetter(doc *gaby.YamlDoc) (api.ResourceCategory, error)
	ResourceTypeGetter(doc *gaby.YamlDoc) (api.ResourceType, error)
	ResourceNameGetter(doc *gaby.YamlDoc) (api.ResourceName, error)
	ResourceIDGetter(doc *gaby.YamlDoc) (string, error)
	RemoveScopeFromResourceName(resourceName api.ResourceName) api.ResourceName
	ScopelessResourceNamePath() api.ResolvedPath
	SetResourceName(doc *gaby.YamlDoc, name string) error
	SetResourceID(doc *gaby.YamlDoc, id string) error
	DeleteResourceID(doc *gaby.YamlDoc) error
	ResourceTypesAreSimilar(resourceTypeA, resourceTypeB api.ResourceType) bool
	TypeDescription() string
	NormalizeName(name string) string
	NameSeparator() string
	ContextPath(contextField string) string
	GetPathRegistry() api.AttributeNameToResourceTypeToPathToVisitorInfoType
	GetAttributeRegistry() api.AttributeNameToAttributeDescriptor
	// MergeKeyForPath returns the merge key field name for the given resource type
	// and array path, if one exists. The path should use dot-separated segments
	// where array indices may be numeric or wildcards. The implementation normalizes
	// numeric indices to wildcards for lookup. Returns ("", false) if no merge key
	// is defined for the path.
	MergeKeyForPath(resourceType api.ResourceType, path string) (string, bool)
	GetToolchainType() workerapi.ToolchainType
}

type ResourceTypeToPathPrefixSetType map[api.ResourceType]map[string]struct{}

func GetResourceCategoryTypeName(doc *gaby.YamlDoc, resourceProvider ResourceProvider) (api.ResourceCategory, api.ResourceType, api.ResourceName, error) {
	resourceInfo, err := GetResourceInfo(doc, resourceProvider)
	if err != nil {
		return "", "", "", err
	}
	return resourceInfo.ResourceCategory, resourceInfo.ResourceType, resourceInfo.ResourceName, nil
}

func GetResourceInfo(doc *gaby.YamlDoc, resourceProvider ResourceProvider) (*api.ResourceInfo, error) {
	var resourceCategory api.ResourceCategory
	var resourceType api.ResourceType
	var resourceName api.ResourceName
	var err error
	resourceCategory, err = resourceProvider.ResourceCategoryGetter(doc)
	if err != nil {
		return nil, errors.Wrap(err, "unable to get resource category for config resource/element")
	}
	resourceType, err = resourceProvider.ResourceTypeGetter(doc)
	if err != nil {
		return nil, errors.Wrap(err, "unable to get resource type for config "+string(resourceCategory))
	}
	resourceName, err = resourceProvider.ResourceNameGetter(doc)
	if err != nil {
		return nil, errors.Wrap(err, "unable to get resource name for config "+string(resourceCategory)+" type "+string(resourceType))
	}
	resourceID, _ := resourceProvider.ResourceIDGetter(doc)
	resourceInfo := &api.ResourceInfo{
		ResourceName:             resourceName,
		ResourceNameWithoutScope: resourceProvider.RemoveScopeFromResourceName(resourceName),
		ResourceType:             resourceType,
		ResourceCategory:         resourceCategory,
		ResourceID:               resourceID,
	}
	return resourceInfo, nil
}

func GetToolchainPath(rp ResourceProvider) string {
	return api.GetToolchainPath(rp.GetToolchainType())
}

type ResourceNameToCategoryTypesMap map[api.ResourceName][]api.ResourceCategoryType
type ResourceCategoryTypeToNamesMap map[api.ResourceCategoryType][]api.ResourceName
type ResourceInfoToDocMap map[api.ResourceInfo]int

// ResourceAndCategoryTypeMaps returns maps of all resources in the provided list of parsed YAML
// documents, from from names to categories+types and categories+types to names.
func ResourceAndCategoryTypeMaps(parsedData gaby.Container, resourceProvider ResourceProvider) (
	resourceMap ResourceNameToCategoryTypesMap,
	categoryTypeMap ResourceCategoryTypeToNamesMap,
	err error,
) {
	resourceMap = make(ResourceNameToCategoryTypesMap)
	categoryTypeMap = make(ResourceCategoryTypeToNamesMap)
	if len(parsedData) == 0 {
		return resourceMap, categoryTypeMap, nil
	}
	visitor := func(_ *gaby.YamlDoc, _ any, _ int, resourceInfo *api.ResourceInfo) (any, []error) {
		categoryType := api.ResourceCategoryType{
			ResourceCategory: resourceInfo.ResourceCategory,
			ResourceType:     resourceInfo.ResourceType,
		}
		resourceMap[resourceInfo.ResourceName] = append(resourceMap[resourceInfo.ResourceName], categoryType)
		categoryTypeMap[categoryType] = append(categoryTypeMap[categoryType], resourceInfo.ResourceName)
		return nil, []error{}
	}
	_, err = VisitResources(parsedData, nil, resourceProvider, visitor)
	return resourceMap, categoryTypeMap, err
}

// ResourceToDocMap returns a map of all resources in the provided list of parsed YAML
// documents to their document index.
func ResourceToDocMap(parsedData gaby.Container, resourceProvider ResourceProvider) (resourceMap ResourceInfoToDocMap, err error) {
	resourceMap = make(ResourceInfoToDocMap)
	if len(parsedData) == 0 {
		return resourceMap, nil
	}
	visitor := func(_ *gaby.YamlDoc, _ any, index int, resourceInfo *api.ResourceInfo) (any, []error) {
		resourceMap[*resourceInfo] = index
		return nil, []error{}
	}
	_, err = VisitResources(parsedData, nil, resourceProvider, visitor)
	return resourceMap, err
}
