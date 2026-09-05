// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package yamlkit

import (
	"slices"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/third_party/gaby"
	"github.com/confighub/sdk/core/workerapi"
)

// This is not in a more general place because it is expected to be used after conversion of other
// formats to YAML.

// ResourceProviderRegistry holds what one provider knows: the path and attribute registries
// its functions read, and the compiled structure its lookups answer from. Every
// ResourceProvider embeds it, so all of that is per-instance and built during construction --
// which is what §5.2 of docs/design/resource-type-specs.md asks for, and why the structure
// lookups below are not package globals.
type ResourceProviderRegistry struct {
	PathRegistry      api.AttributeNameToResourceTypeToPathToVisitorInfoType
	AttributeRegistry api.AttributeNameToAttributeDescriptor

	// toolchainType is this provider's, since a resource type means different things in
	// different toolchains and the compiled specs are keyed by both.
	toolchainType workerapi.ToolchainType

	// specs is the compiled structure, or nil for a toolchain that has declared none. Nil is
	// the ordinary case: a format with no schema declaring keyed lists, unions or freeform
	// maps answers "none" to all three lookups, which is what the eight formats other than
	// Kubernetes each used to say in three hand-written methods of their own.
	specs *CompiledSpecs
}

// NewResourceProviderRegistry creates a registry for a toolchain that declares no structure.
func NewResourceProviderRegistry(toolchainType workerapi.ToolchainType) ResourceProviderRegistry {
	return NewResourceProviderRegistryWithSpecs(toolchainType, nil)
}

// NewResourceProviderRegistryWithSpecs creates a registry whose structure lookups read the
// given compiled specs.
func NewResourceProviderRegistryWithSpecs(toolchainType workerapi.ToolchainType, specs *CompiledSpecs) ResourceProviderRegistry {
	return ResourceProviderRegistry{
		PathRegistry:      make(api.AttributeNameToResourceTypeToPathToVisitorInfoType),
		AttributeRegistry: make(api.AttributeNameToAttributeDescriptor),
		toolchainType:     toolchainType,
		specs:             specs,
	}
}

// MergeKeysForPath implements the ResourceProvider method for every toolchain. See the
// interface for what it answers and why more than one key comes back.
func (r *ResourceProviderRegistry) MergeKeysForPath(resourceType api.ResourceType, path string) ([]string, bool) {
	if r.specs == nil {
		return nil, false
	}
	return r.specs.MergeKeysForPath(r.toolchainType, resourceType, path)
}

// ExclusiveFieldsForPath implements the ResourceProvider method for every toolchain. The path
// may use numeric indices or associative segments; both normalize to wildcards for lookup, as
// they do for merge keys.
func (r *ResourceProviderRegistry) ExclusiveFieldsForPath(resourceType api.ResourceType, path string) (ExclusiveFields, bool) {
	if r.specs == nil {
		return ExclusiveFields{}, false
	}
	return r.specs.ExclusiveFieldsForPath(r.toolchainType, resourceType, path)
}

// IsMapKeyPath implements the ResourceProvider method for every toolchain. The path should end
// with ".*", since the question is always about a path's children.
func (r *ResourceProviderRegistry) IsMapKeyPath(resourceType api.ResourceType, path string) bool {
	if r.specs == nil {
		return false
	}
	return r.specs.IsMapKeyPath(r.toolchainType, resourceType, path)
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
	// MergeKeysForPath returns the merge key field names for the given resource type
	// and array path, if any exist. The path should use dot-separated segments
	// where array indices may be numeric or wildcards. The implementation normalizes
	// numeric indices to wildcards for lookup. Returns (nil, false) if no merge key
	// is defined for the path.
	//
	// More than one key is returned for an array whose elements are identified by a
	// tuple rather than a single field: a Kubernetes container port is identified by
	// its number *and* its protocol, and a topology spread constraint by its topology
	// key *and* its whenUnsatisfiable. Matching such an element on the first field
	// alone pairs elements that are not the same element.
	MergeKeysForPath(resourceType api.ResourceType, path string) ([]string, bool)
	// ExclusiveFieldsForPath returns the mutually exclusive sibling fields of the object
	// at the given path, if the schema declares any. Kubernetes handles the class with
	// patchStrategy:"retainKeys": setting one member of a union has to clear the others,
	// or the result is a resource the API server rejects — a volume with two sources, a
	// Recreate strategy that still carries rollingUpdate. Returns ok=false when the path
	// holds no union, which is the ordinary case.
	ExclusiveFieldsForPath(resourceType api.ResourceType, path string) (ExclusiveFields, bool)
	// IsMapKeyPath returns true if the given path is a freeform map (e.g., labels,
	// annotations) whose children are dynamic keys rather than schema fields.
	// During path normalization, child segments of map paths are converted to wildcards.
	IsMapKeyPath(resourceType api.ResourceType, path string) bool
	GetToolchainType() workerapi.ToolchainType
}

// ExclusiveFields describes a set of sibling fields of which at most one may be present —
// a union, in the schema sense.
//
// Discriminator names the sibling that says which member is valid, where the schema has
// one: a Deployment's strategy has `type`, and `rollingUpdate` is only permitted when it
// reads RollingUpdate. AllowedMember maps each discriminator value to the member it
// permits; a value with no entry permits none. Where there is no discriminator — a Volume's
// source is an inline union with nothing naming it — Discriminator is empty and the member
// the patch sets is the one that survives.
type ExclusiveFields struct {
	Members       []string
	Discriminator string
	AllowedMember map[string]string
}

// IsMember reports whether a field name is one of the union's members.
func (e ExclusiveFields) IsMember(field string) bool {
	return slices.Contains(e.Members, field)
}

// ExclusiveFieldsLookup is ExclusiveFieldsForPath bound to one resource type.
type ExclusiveFieldsLookup func(path string) (ExclusiveFields, bool)

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
		mergeKeys, found := resourceProvider.MergeKeysForPath(attr.ResourceType, arrayPath)
		if !found || len(mergeKeys) == 0 {
			continue
		}
		// The first key names the element for this purpose: it is the identifying one
		// (containerPort before protocol, port before protocol), and the property this
		// builds is a hint for matching a provider, not an exact identity.
		mergeKey := mergeKeys[0]
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
