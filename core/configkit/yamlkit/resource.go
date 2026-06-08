// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package yamlkit

import (
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/third_party/gaby"
	"github.com/confighub/sdk/core/workerapi"
)

// This is not in a more general place because it is expected to be used after conversion of other
// formats to YAML.

// ResourceProviderRegistry holds the path and attribute registries common to all
// ResourceProvider implementations.
type ResourceProviderRegistry struct {
	PathRegistry      api.AttributeNameToResourceTypeToPathToVisitorInfoType
	AttributeRegistry api.AttributeNameToAttributeDescriptor
}

// NewResourceProviderRegistry creates a new ResourceProviderRegistry with initialized maps.
func NewResourceProviderRegistry() ResourceProviderRegistry {
	return ResourceProviderRegistry{
		PathRegistry:      make(api.AttributeNameToResourceTypeToPathToVisitorInfoType),
		AttributeRegistry: make(api.AttributeNameToAttributeDescriptor),
	}
}

func (r *ResourceProviderRegistry) GetPathRegistry() api.AttributeNameToResourceTypeToPathToVisitorInfoType {
	return r.PathRegistry
}

func (r *ResourceProviderRegistry) GetAttributeRegistry() api.AttributeNameToAttributeDescriptor {
	return r.AttributeRegistry
}

func (r *ResourceProviderRegistry) GetRegistry() *ResourceProviderRegistry {
	return r
}

// The ResourceProvider interface is used to perform toolchain-specific operations.
type ResourceProvider interface {
	DefaultResourceCategory() api.ResourceCategory
	ResourceCategoryGetter(doc *gaby.YamlDoc) (api.ResourceCategory, error)
	ResourceTypeGetter(doc *gaby.YamlDoc) (api.ResourceType, error)
	ResourceNameGetter(doc *gaby.YamlDoc) (api.ResourceName, error)
	// ResourceNameStableCoreGetter returns the stable core of the resource name, with
	// generated prefixes and suffixes stripped. Returns empty string if not present.
	ResourceNameStableCoreGetter(doc *gaby.YamlDoc) (api.ResourceName, error)
	RemoveScopeFromResourceName(resourceName api.ResourceName) api.ResourceName
	ScopelessResourceNamePath() api.ResolvedPath
	SetResourceName(doc *gaby.YamlDoc, name string) error
	ResourceTypesAreSimilar(resourceTypeA, resourceTypeB api.ResourceType) bool
	TypeDescription() string
	NormalizeName(name string) string
	NameSeparator() string
	ContextPath(contextField string) string
	GetPathRegistry() api.AttributeNameToResourceTypeToPathToVisitorInfoType
	GetAttributeRegistry() api.AttributeNameToAttributeDescriptor
	GetRegistry() *ResourceProviderRegistry
	// MergeKeyForPath returns the merge key field name for the given resource type
	// and array path, if one exists. The path should use dot-separated segments
	// where array indices may be numeric or wildcards. The implementation normalizes
	// numeric indices to wildcards for lookup. Returns ("", false) if no merge key
	// is defined for the path.
	MergeKeyForPath(resourceType api.ResourceType, path string) (string, bool)
	// IsMapKeyPath returns true if the given path is a freeform map (e.g., labels,
	// annotations) whose children are dynamic keys rather than schema fields.
	// During path normalization, child segments of map paths are converted to wildcards.
	IsMapKeyPath(resourceType api.ResourceType, path string) bool
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
	resourceNameStableCore, _ := resourceProvider.ResourceNameStableCoreGetter(doc)
	resourceInfo := &api.ResourceInfo{
		ResourceName:             resourceName,
		ResourceNameWithoutScope: resourceProvider.RemoveScopeFromResourceName(resourceName),
		ResourceNameStableCore:   resourceNameStableCore,
		ResourceType:             resourceType,
		ResourceCategory:         resourceCategory,
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

// FindResourceDoc finds the document in parsedData that best matches the given target
// ResourceInfo. It uses the same matching hierarchy as ComputeMutations:
//  1. Exact ResourceName or ResourceNameWithoutScope match
//  2. ResourceTypesAreSimilar as a prerequisite for any match
//
// Returns the matching doc and its ResourceInfo, or (nil, nil) if no match is found.
func FindResourceDoc(
	parsedData gaby.Container,
	resourceProvider ResourceProvider,
	target *api.ResourceInfo,
) (*gaby.YamlDoc, *api.ResourceInfo) {
	var bestDoc *gaby.YamlDoc
	var bestInfo *api.ResourceInfo

	visitor := func(doc *gaby.YamlDoc, output any, _ int, ri *api.ResourceInfo) (any, []error) {
		if !resourceProvider.ResourceTypesAreSimilar(ri.ResourceType, target.ResourceType) {
			return output, nil
		}

		// Exact name match (full or without scope)
		if bestDoc == nil {
			if ri.ResourceName == target.ResourceName ||
				ri.ResourceNameWithoutScope == target.ResourceNameWithoutScope {
				bestDoc = doc
				bestInfo = ri
			}
		}

		return output, nil
	}

	_, _ = VisitResources(parsedData, nil, resourceProvider, visitor)
	return bestDoc, bestInfo
}

// EnrichMergeKeysFromDoc extracts merge keys from the resolved path of an AttributeValue
// and adds them as NeededPreferred properties. For each numeric array index in the path,
// it looks up the merge key via MergeKeyForPath and reads the value from the document.
// For example, path "spec.template.spec.volumes.1.configMap.name" with merge key
// "name"="config" at volumes[1] yields NeededPreferred["Name"] = "config".
func EnrichMergeKeysFromDoc(doc *gaby.YamlDoc, resourceProvider ResourceProvider, attr *api.AttributeValue) {
	segments := gaby.DotPathToSlice(string(attr.Path))
	for si, seg := range segments {
		if len(seg) == 0 || seg[0] < '0' || seg[0] > '9' {
			continue
		}
		// Build wildcard path for merge key lookup (copy to avoid corrupting segments)
		wildcardSegments := make([]string, si+1)
		copy(wildcardSegments, segments[:si])
		wildcardSegments[si] = "*"
		arrayPath := JoinPathSegments(wildcardSegments)
		mergeKey, found := resourceProvider.MergeKeyForPath(attr.ResourceType, arrayPath)
		if !found || mergeKey == "" {
			continue
		}
		mergeKeyPath := JoinPathSegments(segments[:si+1]) + "." + EscapeDotsInPathSegment(mergeKey)
		mkValue, mkFound, _ := YamlSafePathGetValue[string](doc, api.ResolvedPath(mergeKeyPath), true)
		if !mkFound || mkValue == "" {
			continue
		}
		if attr.Details == nil {
			attr.Details = &api.AttributeDetails{}
		}
		if attr.Details.NeededPreferred == nil {
			attr.Details.NeededPreferred = make(map[string]string)
		}
		propKey := strings.ToUpper(mergeKey[:1]) + mergeKey[1:]
		attr.Details.NeededPreferred[propKey] = mkValue
	}
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
