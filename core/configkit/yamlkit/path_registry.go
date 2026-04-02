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
func AttributeDetailsEqual(details1, details2 *api.AttributeDetails, compareFunctions bool) bool {
	isDescription1 := (details1 != nil && details1.Description != "")
	isDescription2 := (details2 != nil && details2.Description != "")
	if isDescription1 != isDescription2 ||
		(isDescription1 && isDescription2 &&
			details1.Description != details2.Description) {
		return false
	}
	// Compare IsNeeded/IsProvided flags
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
	// Compare property maps
	if !stringMapsEqual(details1.ProvidedProperties, details2.ProvidedProperties) {
		return false
	}
	if !stringMapsEqual(details1.NeededRequired, details2.NeededRequired) {
		return false
	}
	if !stringMapsEqual(details1.NeededPreferred, details2.NeededPreferred) {
		return false
	}
	if !compareFunctions {
		return true
	}
	if !FunctionInvocationsEqual(details1.GetterInvocation, details2.GetterInvocation) {
		return false
	}
	if len(details1.SetterInvocations) != len(details2.SetterInvocations) {
		return false
	}
	for i := range details1.SetterInvocations {
		if !FunctionInvocationsEqual(&details1.SetterInvocations[i], &details2.SetterInvocations[i]) {
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
func VisitorInfoEqual(pathVisitorInfo1, pathVisitorInfo2 *api.PathVisitorInfo, compareFunctions bool) bool {
	return pathVisitorInfo1.AttributeName == pathVisitorInfo2.AttributeName &&
		pathVisitorInfo1.DataType == pathVisitorInfo2.DataType &&
		pathVisitorInfo1.EmbeddedAccessorType == pathVisitorInfo2.EmbeddedAccessorType &&
		pathVisitorInfo1.EmbeddedAccessorConfig == pathVisitorInfo2.EmbeddedAccessorConfig &&
		AttributeDetailsEqual(pathVisitorInfo1.Details, pathVisitorInfo2.Details, compareFunctions)
}

func setFunctionInvocationsInVisitorPathInfo(
	pathInfo *api.PathVisitorInfo,
	getterFunctionInvocation *api.FunctionInvocation,
	setterFunctionInvocation *api.FunctionInvocation,
) {
	if getterFunctionInvocation == nil && setterFunctionInvocation == nil {
		return
	}
	if pathInfo.Details == nil {
		pathInfo.Details = &api.AttributeDetails{}
	}
	// The getter should be the same.
	if getterFunctionInvocation != nil {
		if pathInfo.Details.GetterInvocation != nil &&
			!FunctionInvocationsEqual(pathInfo.Details.GetterInvocation, getterFunctionInvocation) {
			slog.Error("different getter function invocations registered",
				"existing", pathInfo.Details.GetterInvocation, "new", getterFunctionInvocation)
		}
		pathInfo.Details.GetterInvocation = getterFunctionInvocation
	}
	if setterFunctionInvocation != nil {
		found := false
		for _, setterInvocation := range pathInfo.Details.SetterInvocations {
			// The function name could be different and/or the argument values could be different.
			// Example: resource references that could refer to multiple resource types.
			if FunctionInvocationsEqual(&setterInvocation, setterFunctionInvocation) {
				found = true
				break
			}
		}
		if !found {
			pathInfo.Details.SetterInvocations = append(pathInfo.Details.SetterInvocations, *setterFunctionInvocation)
		}
	}
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
		if existing, ok := pathInfo.Details.NeededRequired[k]; ok && existing != v {
			// Multiple different values for the same required key means
			// the field can reference multiple types — mark as empty to
			// prevent subsequent registrations from re-adding a value.
			pathInfo.Details.NeededRequired[k] = ""
		} else if ok && existing == "" {
			// Already marked as multi-value — don't overwrite
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
	var getterFunctionInvocation *api.FunctionInvocation
	var setterFunctionInvocation *api.FunctionInvocation
	var enricher AttributeEnricher
	if details != nil {
		getterFunctionInvocation = details.GetterInvocation
		setterFunctionInvocation = details.SetterInvocation
		enricher = details.Enricher
	}
	_, ok := registry[resourceType]
	if !ok {
		registry[resourceType] = make(api.PathToVisitorInfoType)
		for path, pathInfo := range pathInfos {
			registry[resourceType][path] = pathInfo
			setFunctionInvocationsInVisitorPathInfo(pathInfo, getterFunctionInvocation, setterFunctionInvocation)
			if details != nil {
				setNeedsProvidesDetailsInVisitorPathInfo(pathInfo, details.AttributeNeedsProvidesDetails)
			}
			if enricher != nil {
				pathInfo.Enricher = enricher
			}
		}
		return
	}

	// Some paths could already be registered under the same attribute name.
	// Example: resource references that could refer to multiple resource types.
	for path, newPathInfo := range pathInfos {
		oldPathInfo, present := registry[resourceType][path]
		if present {
			if !VisitorInfoEqual(oldPathInfo, newPathInfo, false) {
				slog.Error("info mismatch for path",
					"path", newPathInfo.Path, "resourceType", resourceType,
					"newPathInfo", newPathInfo, "oldPathInfo", oldPathInfo,
					"newDetails", newPathInfo.Details, "oldDetails", oldPathInfo.Details,
				)
			}
			newPathInfo = oldPathInfo
		} else {
			registry[resourceType][path] = newPathInfo
		}
		setFunctionInvocationsInVisitorPathInfo(newPathInfo, getterFunctionInvocation, setterFunctionInvocation)
		if details != nil {
			setNeedsProvidesDetailsInVisitorPathInfo(newPathInfo, details.AttributeNeedsProvidesDetails)
		}
		if enricher != nil {
			newPathInfo.Enricher = enricher
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
	GetterInvocation *api.FunctionInvocation
	SetterInvocation *api.FunctionInvocation
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

	var getterFunctionInvocation *api.FunctionInvocation
	var setterFunctionInvocation *api.FunctionInvocation
	if details != nil {
		getterFunctionInvocation = details.GetterInvocation
		setterFunctionInvocation = details.SetterInvocation
	}

	pathRegistry := resourceProvider.GetPathRegistry()
	_, present := pathRegistry[attributeName]
	if !present {
		pathRegistry[attributeName] = make(api.ResourceTypeToPathToVisitorInfoType)
	}

	attributeRegistry := resourceProvider.GetAttributeRegistry()
	if _, exists := attributeRegistry[attributeName]; !exists {
		attributeRegistry[attributeName] = &api.AttributeDescriptor{
			AttributeName: attributeName,
			AttributeVisitorDetails: api.AttributeVisitorDetails{
				AttributeDetails: api.AttributeDetails{
					GetterInvocation: getterFunctionInvocation,
					SetterInvocations: func() []api.FunctionInvocation {
						if setterFunctionInvocation != nil {
							return []api.FunctionInvocation{*setterFunctionInvocation}
						}
						return nil
					}(),
				},
			},
		}
	} else {
		descriptor := attributeRegistry[attributeName]
		if getterFunctionInvocation != nil && descriptor.GetterInvocation == nil {
			descriptor.GetterInvocation = getterFunctionInvocation
		}
		if setterFunctionInvocation != nil {
			found := false
			for _, setter := range descriptor.SetterInvocations {
				if FunctionInvocationsEqual(&setter, setterFunctionInvocation) {
					found = true
					break
				}
			}
			if !found {
				descriptor.SetterInvocations = append(descriptor.SetterInvocations, *setterFunctionInvocation)
			}
		}
	}

	// Always normalize paths so that registered paths and lookup paths use the same form.
	newPathInfos := make(api.PathToVisitorInfoType)
	for path, pathInfo := range pathInfos {
		normalizedPath := normalizePath(resourceProvider, resourceType, path)
		newPathInfo := *pathInfo // deep copy
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
	return getRegisteredPathsByFlag(resourceProvider, true, false)
}

// GetRegisteredProvidedPaths returns a combined registry of all paths marked as IsProvided
// across all attributes in the path registry.
func GetRegisteredProvidedPaths(resourceProvider ResourceProvider) api.ResourceTypeToPathToVisitorInfoType {
	return getRegisteredPathsByFlag(resourceProvider, false, true)
}

func getRegisteredPathsByFlag(resourceProvider ResourceProvider, needed, provided bool) api.ResourceTypeToPathToVisitorInfoType {
	combined := make(api.ResourceTypeToPathToVisitorInfoType)
	pathRegistry := resourceProvider.GetPathRegistry()
	for _, resourceTypeToPathToVisitorInfo := range pathRegistry {
		for rt, pathToVisitorInfo := range resourceTypeToPathToVisitorInfo {
			for path, info := range pathToVisitorInfo {
				if info.Details != nil && ((needed && info.Details.IsNeeded) || (provided && info.Details.IsProvided)) {
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
	if src.Details.GetterInvocation != nil && dst.Details.GetterInvocation == nil {
		dst.Details.GetterInvocation = src.Details.GetterInvocation
	}
	for _, setter := range src.Details.SetterInvocations {
		found := false
		for _, existingSetter := range dst.Details.SetterInvocations {
			if FunctionInvocationsEqual(&existingSetter, &setter) {
				found = true
				break
			}
		}
		if !found {
			dst.Details.SetterInvocations = append(dst.Details.SetterInvocations, setter)
		}
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
