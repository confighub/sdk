// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package yamlkit

import (
	"log/slog"

	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/third_party/gaby"
)

// FunctionInvocationsEqual reports whether two function invocations match.
func FunctionInvocationsEqual(fi1, fi2 *api.FunctionInvocation) bool {
	if (fi1 == nil) != (fi2 == nil) {
		return false
	}
	if fi1 == nil {
		return true
	}
	if fi1.FunctionName != fi2.FunctionName || len(fi1.Arguments) != len(fi2.Arguments) {
		return false
	}
	for i, _ := range fi1.Arguments {
		if fi1.Arguments[i].ParameterName != fi2.Arguments[i].ParameterName ||
			fi1.Arguments[i].Value != fi2.Arguments[i].Value {
			return false
		}
	}
	return true
}

// AttributeDetailsEqual reports whether two sets of attribute details, optionally including
// getter and setter invocations, match.
//
// Needs/provides properties are compared for compatibility rather than equality: a path
// registered twice with different required values accepts either, and registerPaths unions them.
// A reference field naming any workload controller is registered once per controller, and those
// registrations agreeing about everything except which type they name is the normal case, not a
// mismatch to report.
func AttributeDetailsEqual(details1, details2 *api.AttributeDetails) bool {
	isDescription1 := (details1 != nil && details1.Description != "")
	isDescription2 := (details2 != nil && details2.Description != "")
	if isDescription1 != isDescription2 ||
		(isDescription1 && isDescription2 &&
			details1.Description != details2.Description) {
		return false
	}
	isNeeded1 := details1 != nil && details1.IsNeeded
	isNeeded2 := details2 != nil && details2.IsNeeded
	if isNeeded1 != isNeeded2 {
		return false
	}
	isProvided1 := details1 != nil && details1.IsProvided
	isProvided2 := details2 != nil && details2.IsProvided
	if isProvided1 != isProvided2 {
		return false
	}
	if !stringMapsEqual(details1.ProvidedProperties, details2.ProvidedProperties) {
		return false
	}
	if !requiredPropertiesCompatible(details1.NeededRequired, details2.NeededRequired) {
		return false
	}
	return stringMapsEqual(details1.NeededPreferred, details2.NeededPreferred)
}

// requiredPropertiesCompatible reports whether two sets of required properties can be unioned:
// same keys, and values that are alternatives of one requirement rather than two requirements
// that disagree about anything else.
func requiredPropertiesCompatible(m1, m2 map[string]string) bool {
	if len(m1) != len(m2) {
		return false
	}
	for k := range m1 {
		if _, present := m2[k]; !present {
			return false
		}
	}
	return true
}

func stringMapsEqual(m1, m2 map[string]string) bool {
	if len(m1) != len(m2) {
		return false
	}
	for k, v := range m1 {
		if m2[k] != v {
			return false
		}
	}
	return true
}

// VisitorInfoEqual reports whether two path visitor specifications, optionally including
// getter and setter invocations, match.
func VisitorInfoEqual(pathVisitorInfo1, pathVisitorInfo2 *api.PathVisitorInfo) bool {
	return pathVisitorInfo1.AttributeName == pathVisitorInfo2.AttributeName &&
		pathVisitorInfo1.DataType == pathVisitorInfo2.DataType &&
		pathVisitorInfo1.EmbeddedAccessorType == pathVisitorInfo2.EmbeddedAccessorType &&
		pathVisitorInfo1.EmbeddedAccessorConfig == pathVisitorInfo2.EmbeddedAccessorConfig &&
		AttributeDetailsEqual(pathVisitorInfo1.Details, pathVisitorInfo2.Details)
}

// setNeedsProvidesDetailsInVisitorPathInfo merges AttributeNeedsProvidesDetails from
// registration details into a PathVisitorInfo's Details.
func setNeedsProvidesDetailsInVisitorPathInfo(pathInfo *api.PathVisitorInfo, npd api.AttributeNeedsProvidesDetails) {
	if pathInfo.Details == nil {
		pathInfo.Details = &api.AttributeDetails{}
	}
	for k, v := range npd.ProvidedProperties {
		if pathInfo.Details.ProvidedProperties == nil {
			pathInfo.Details.ProvidedProperties = make(map[string]string)
		}
		pathInfo.Details.ProvidedProperties[k] = v
	}
	for k, v := range npd.NeededRequired {
		if pathInfo.Details.NeededRequired == nil {
			pathInfo.Details.NeededRequired = make(map[string]string)
		}
		if existing, ok := pathInfo.Details.NeededRequired[k]; ok {
			// The same path registered with a different value for a required key is a field
			// that accepts either -- an HPA's scaleTargetRef naming any workload controller.
			// The requirement is the union, not the absence of one.
			pathInfo.Details.NeededRequired[k] = api.UnionPropertyValue(existing, v)
		} else {
			pathInfo.Details.NeededRequired[k] = v
		}
	}
	for k, v := range npd.NeededPreferred {
		if pathInfo.Details.NeededPreferred == nil {
			pathInfo.Details.NeededPreferred = make(map[string]string)
		}
		pathInfo.Details.NeededPreferred[k] = v
	}
}

func registerPaths(
	registry api.ResourceTypeToPathToVisitorInfoType,
	resourceType api.ResourceType,
	pathInfos api.PathToVisitorInfoType,
	details *AttributeRegistrationDetails,
) {
	var enricher AttributeEnricher
	if details != nil {
		enricher = details.Enricher
	}
	if _, ok := registry[resourceType]; !ok {
		registry[resourceType] = make(api.PathToVisitorInfoType)
	}

	// Some paths could already be registered under the same attribute name.
	// Example: resource references that could refer to multiple resource types.
	for path, newPathInfo := range pathInfos {
		// A registration states its needs/provides properties in details, not on the path
		// info, so apply them before comparing. Otherwise the comparison is between an
		// incoming registration that has no properties yet and a stored one that has
		// accumulated them, which no second registration of a path can ever satisfy.
		if details != nil {
			setNeedsProvidesDetailsInVisitorPathInfo(newPathInfo, details.AttributeNeedsProvidesDetails)
		}
		registeredPathInfo := newPathInfo
		if oldPathInfo, present := registry[resourceType][path]; present {
			if !VisitorInfoEqual(oldPathInfo, newPathInfo) {
				slog.Error("info mismatch for path",
					"path", newPathInfo.Path, "resourceType", resourceType,
					"newPathInfo", newPathInfo, "oldPathInfo", oldPathInfo,
					"newDetails", newPathInfo.Details, "oldDetails", oldPathInfo.Details,
				)
			}
			registeredPathInfo = oldPathInfo
			if details != nil {
				setNeedsProvidesDetailsInVisitorPathInfo(registeredPathInfo, details.AttributeNeedsProvidesDetails)
			}
		} else {
			registry[resourceType][path] = newPathInfo
		}
		if enricher != nil {
			registeredPathInfo.Enricher = enricher
		}
	}
}

// AttributeEnricher is a function that enriches an AttributeValue with properties
// after it is extracted by a visitor. It receives the resource doc for context and
// a flag indicating whether the value is a provided value. It populates
// ProvidedProperties, NeededRequired, and/or NeededPreferred on the attribute's Details.
type AttributeEnricher func(doc *gaby.YamlDoc, attrInfo *api.AttributeValue, isProvided bool) error

// AttributeRegistrationDetails specifies getter/setter invocations and an optional
// Enricher function for use when registering paths via RegisterPathsByAttributeName.
type AttributeRegistrationDetails struct {
	api.AttributeNeedsProvidesDetails
	Enricher AttributeEnricher
}

// RegisterPathsByAttributeName registers the specified path visitor specifications under the
// designated attribute name and resource type, and adds the provided getter and setter invocations,
// merging with existing registrations at the same paths, if any. If requested, the registered paths
// will be normalized so that associative lookups and array indices will be converted to wildcards,
// which is desired when matching all paths to the attribute. AttributeNameResourceName is used for
// references to resource names. Other attribute names are used for specific setters and/or getters,
// especially for attributes that appear in multiple resource types and/or locations.
// Provided values are special in that they represent sources of values for attributes of the specified
// attribute name, though they are logically distinct kinds of attributes.
func RegisterPathsByAttributeName(
	resourceProvider ResourceProvider,
	attributeName api.AttributeName,
	resourceType api.ResourceType,
	pathInfos api.PathToVisitorInfoType,
	details *AttributeRegistrationDetails,
	isNeeded bool,
	isProvided bool,
) {
	// Set IsNeeded/IsProvided on each pathInfo's Details
	if isNeeded || isProvided {
		for _, pathInfo := range pathInfos {
			if pathInfo.Details == nil {
				pathInfo.Details = &api.AttributeDetails{}
			}
			if isNeeded {
				pathInfo.Details.IsNeeded = true
			}
			if isProvided {
				pathInfo.Details.IsProvided = true
			}
		}
	}

	pathRegistry := resourceProvider.GetPathRegistry()
	_, present := pathRegistry[attributeName]
	if !present {
		pathRegistry[attributeName] = make(api.ResourceTypeToPathToVisitorInfoType)
	}

	attributeRegistry := resourceProvider.GetAttributeRegistry()
	if _, exists := attributeRegistry[attributeName]; !exists {
		attributeRegistry[attributeName] = &api.AttributeDescriptor{AttributeName: attributeName}
	}
	// Always normalize paths so that registered paths and lookup paths use the same form.
	newPathInfos := make(api.PathToVisitorInfoType)
	for path, pathInfo := range pathInfos {
		normalizedPath := normalizePath(resourceProvider, resourceType, path)
		newPathInfo := *pathInfo
		// Details too, and not the pointer to them: registration merges into the details it
		// stores, so a caller that registers one pathInfos map for several resource types
		// would otherwise have every one of those registrations merging into a single shared
		// set of details.
		newPathInfo.Details = api.DeepCopyAttributeDetails(pathInfo.Details)
		newPathInfos[normalizedPath] = &newPathInfo
	}
	registerPaths(
		pathRegistry[attributeName],
		resourceType,
		newPathInfos,
		details,
	)
}

// GetPathRegistryForAttributeName returns the registry for the specified attribute to pass
// to a visitor function. If the attribute has a non-empty AttributeGroup, the registries for
// all attributes in the group are combined and returned.
func GetPathRegistryForAttributeName(
	resourceProvider ResourceProvider,
	attributeName api.AttributeName,
) api.ResourceTypeToPathToVisitorInfoType {
	attributeRegistry := resourceProvider.GetAttributeRegistry()
	if descriptor, exists := attributeRegistry[attributeName]; exists && len(descriptor.AttributeGroup) > 0 {
		combined := make(api.ResourceTypeToPathToVisitorInfoType)
		pathRegistry := resourceProvider.GetPathRegistry()
		for _, groupAttrName := range descriptor.AttributeGroup {
			if rtMap, present := pathRegistry[groupAttrName]; present {
				for rt, pathMap := range rtMap {
					if _, ok := combined[rt]; !ok {
						combined[rt] = make(api.PathToVisitorInfoType)
					}
					for path, info := range pathMap {
						combined[rt][path] = info
					}
				}
			}
		}
		return combined
	}

	pathRegistry := resourceProvider.GetPathRegistry()
	var resourceTypeToPathToVisitorInfo api.ResourceTypeToPathToVisitorInfoType
	resourceTypeToPathToVisitorInfo, _ = pathRegistry[attributeName]
	return resourceTypeToPathToVisitorInfo
}

// GetPathRegistryForAttributeNameByProperty returns the paths registered under an attribute
// whose needs/provides properties carry propertyKey with propertyValue, on either side: a
// provided path offering it, or a needed path requiring it.
//
// This is how a reference selects the paths that point at one resource type. Splitting the
// attribute into a name per target would answer the same question by string convention, which is
// a second encoding of what the properties already say -- and a convention spelled differently in
// two places matches nothing, silently.
func GetPathRegistryForAttributeNameByProperty(
	resourceProvider ResourceProvider,
	attributeName api.AttributeName,
	propertyKey string,
	propertyValue string,
) api.ResourceTypeToPathToVisitorInfoType {
	matching := make(api.ResourceTypeToPathToVisitorInfoType)
	for resourceType, pathToVisitorInfo := range GetPathRegistryForAttributeName(resourceProvider, attributeName) {
		for path, info := range pathToVisitorInfo {
			if info.Details == nil {
				continue
			}
			provided := info.Details.ProvidedProperties[propertyKey] == propertyValue
			needed := api.PropertyValueSatisfiedBy(info.Details.NeededRequired[propertyKey], propertyValue)
			if _, requires := info.Details.NeededRequired[propertyKey]; !requires {
				needed = false
			}
			if !provided && !needed {
				continue
			}
			if matching[resourceType] == nil {
				matching[resourceType] = make(api.PathToVisitorInfoType)
			}
			matching[resourceType][path] = info
		}
	}
	if len(matching) == 0 {
		return nil
	}
	return matching
}

// ResourceTypesForAttribute returns a list of resource types associated with the specified attribute.
func ResourceTypesForAttribute(attributeName api.AttributeName, resourceProvider ResourceProvider) []api.ResourceType {
	resourceTypeToPaths := GetPathRegistryForAttributeName(resourceProvider, attributeName)
	resourceTypes := make([]api.ResourceType, 0, len(resourceTypeToPaths))
	for resourceType := range resourceTypeToPaths {
		resourceTypes = append(resourceTypes, resourceType)
		// Just report all resource types if a wildcard is present
		if resourceType == api.ResourceTypeAny {
			break
		}
	}
	return resourceTypes
}

// ResourceTypesForPathMap returns a list of resource types from a path map.
func ResourceTypesForPathMap(pathMap map[api.ResourceType][]string) []api.ResourceType {
	resourceTypes := make([]api.ResourceType, 0, len(pathMap))
	for resourceType := range pathMap {
		resourceTypes = append(resourceTypes, resourceType)
		// Just report all resource types if a wildcard is present
		if resourceType == api.ResourceTypeAny {
			break
		}
	}
	return resourceTypes
}

// GetVisitorMapForPath is used to get visitor info for a resolved path.
func GetVisitorMapForPath(resourceProvider ResourceProvider, rt api.ResourceType, path api.UnresolvedPath) api.ResourceTypeToPathToVisitorInfoType {
	if rt == "" {
		rt = api.ResourceTypeAny
	}

	visitorInfo := GetPathVisitorInfo(resourceProvider, rt, path)
	if visitorInfo == nil {
		visitorInfo = &api.PathVisitorInfo{}
		visitorInfo.AttributeName = api.AttributeNameNone
		visitorInfo.Path = path
	} else {
		// Create a copy to modify
		specificVisitorInfo := *visitorInfo
		visitorInfo = &specificVisitorInfo
		// Path may be overridden below
	}
	if PathIsResolved(string(path), true) {
		visitorInfo.ResolvedPath = api.ResolvedPath(path)
	} else {
		visitorInfo.Path = path
	}
	resourceTypeToPaths := api.ResourceTypeToPathToVisitorInfoType{
		rt: {path: visitorInfo},
	}
	return resourceTypeToPaths
}

// GetPathVisitorInfo returns the path visitor specification for the specified path within the
// specified resource type to pass to a visitor function. It searches all attribute names in the
// path registry for the normalized path.
// GetPathVisitorInfo returns the PathVisitorInfo for the specified path. It searches all
// attribute names in the path registry, checking the specific resource type across all
// attributes first, then falling back to ResourceTypeAny. This ensures a specific resource
// type match always takes priority. The first match provides the base Details (getter/setter
// invocations). IsNeeded/IsProvided flags and Enricher are collected from all matches.
// Getter/setter invocations are NOT merged across attribute names because they carry
// resource-type-specific arguments.
func GetPathVisitorInfo(resourceProvider ResourceProvider, resourceType api.ResourceType, path api.UnresolvedPath) *api.PathVisitorInfo {
	normalizedPath := normalizePath(resourceProvider, resourceType, path)

	var result *api.PathVisitorInfo
	pathRegistry := resourceProvider.GetPathRegistry()

	// Outer loop: check specific resource type first, then wildcard
	for _, rt := range []api.ResourceType{resourceType, api.ResourceTypeAny} {
		for _, resourceTypeToPathToVisitorInfo := range pathRegistry {
			pathMap, present := resourceTypeToPathToVisitorInfo[rt]
			if !present {
				continue
			}
			visitorInfo, found := pathMap[normalizedPath]
			if !found {
				continue
			}
			if result == nil {
				resultCopy := *visitorInfo
				if resultCopy.Details != nil {
					detailsCopy := *resultCopy.Details
					result = &resultCopy
					result.Details = &detailsCopy
				} else {
					result = &resultCopy
				}
			} else {
				mergeDetails(result, visitorInfo)
				if result.Enricher == nil && visitorInfo.Enricher != nil {
					result.Enricher = visitorInfo.Enricher
				}
			}
		}
	}
	return result
}

// GetRegisteredNeededPaths returns a combined registry of all paths marked as IsNeeded
// across all attributes in the path registry.
func GetRegisteredNeededPaths(resourceProvider ResourceProvider) api.ResourceTypeToPathToVisitorInfoType {
	return getRegisteredPathsByFlag(resourceProvider, true, false, nil)
}

// GetRegisteredProvidedPaths returns a combined registry of all paths marked as IsProvided
// across all attributes in the path registry.
func GetRegisteredProvidedPaths(resourceProvider ResourceProvider) api.ResourceTypeToPathToVisitorInfoType {
	return getRegisteredPathsByFlag(resourceProvider, false, true, nil)
}

// GetRegisteredNeededPathsByProperty returns a combined registry of all paths marked as
// IsNeeded whose Details.NeededRequired map contains every key listed in neededRequired.
// Values of those required keys are not checked — only presence. This is useful for finding
// paths that participate in a particular kind of match (e.g., all resource references via
// the "ResourceType" key) regardless of whether the current value at the path is a
// placeholder.
func GetRegisteredNeededPathsByProperty(resourceProvider ResourceProvider, neededRequired []string) api.ResourceTypeToPathToVisitorInfoType {
	return getRegisteredPathsByFlag(resourceProvider, true, false, neededRequired)
}

func getRegisteredPathsByFlag(resourceProvider ResourceProvider, needed, provided bool, neededRequired []string) api.ResourceTypeToPathToVisitorInfoType {
	combined := make(api.ResourceTypeToPathToVisitorInfoType)
	pathRegistry := resourceProvider.GetPathRegistry()
	for _, resourceTypeToPathToVisitorInfo := range pathRegistry {
		for rt, pathToVisitorInfo := range resourceTypeToPathToVisitorInfo {
			for path, info := range pathToVisitorInfo {
				if info.Details == nil {
					continue
				}
				if !((needed && info.Details.IsNeeded) || (provided && info.Details.IsProvided)) {
					continue
				}
				if len(neededRequired) > 0 {
					hasAll := true
					for _, key := range neededRequired {
						if _, ok := info.Details.NeededRequired[key]; !ok {
							hasAll = false
							break
						}
					}
					if !hasAll {
						continue
					}
				}
				if _, ok := combined[rt]; !ok {
					combined[rt] = make(api.PathToVisitorInfoType)
				}
				existing, exists := combined[rt][path]
				if !exists {
					// Copy to avoid modifying the registry's PathVisitorInfo
					infoCopy := *info
					if info.Details != nil {
						detailsCopy := *info.Details
						infoCopy.Details = &detailsCopy
					}
					combined[rt][path] = &infoCopy
				} else if existing != info {
					// The same path may appear under multiple attribute names with
					// different subsets of setter/getter invocations. Merge them.
					mergeDetails(existing, info)
				}
			}
		}
	}
	return combined
}

// mergeDetails merges getter/setter invocations, properties, and flags from src into dst's Details.
func mergeDetails(dst, src *api.PathVisitorInfo) {
	if src.Details == nil {
		return
	}
	if dst.Details == nil {
		dst.Details = &api.AttributeDetails{}
	}
	// Merge property maps (don't overwrite existing entries)
	for k, v := range src.Details.ProvidedProperties {
		if dst.Details.ProvidedProperties == nil {
			dst.Details.ProvidedProperties = make(map[string]string)
		}
		if _, exists := dst.Details.ProvidedProperties[k]; !exists {
			dst.Details.ProvidedProperties[k] = v
		}
	}
	for k, v := range src.Details.NeededRequired {
		if dst.Details.NeededRequired == nil {
			dst.Details.NeededRequired = make(map[string]string)
		}
		if _, exists := dst.Details.NeededRequired[k]; !exists {
			dst.Details.NeededRequired[k] = v
		}
	}
	for k, v := range src.Details.NeededPreferred {
		if dst.Details.NeededPreferred == nil {
			dst.Details.NeededPreferred = make(map[string]string)
		}
		if _, exists := dst.Details.NeededPreferred[k]; !exists {
			dst.Details.NeededPreferred[k] = v
		}
	}
	// Merge flags using OR
	dst.Details.IsNeeded = dst.Details.IsNeeded || src.Details.IsNeeded
	dst.Details.IsProvided = dst.Details.IsProvided || src.Details.IsProvided
}
