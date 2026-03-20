// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package yamlkit

import (
	"log/slog"

	"github.com/confighub/sdk/core/function/api"
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
	if !compareFunctions {
		return true
	}
	if !FunctionInvocationsEqual(details1.GetterInvocation, details2.GetterInvocation) {
		return false
	}
	if len(details1.SetterInvocations) != len(details2.SetterInvocations) {
		return false
	}
	for i, _ := range details1.SetterInvocations {
		if !FunctionInvocationsEqual(&details1.SetterInvocations[i], &details2.SetterInvocations[i]) {
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

func registerPaths(
	registry api.ResourceTypeToPathToVisitorInfoType,
	resourceType api.ResourceType,
	pathInfos api.PathToVisitorInfoType,
	getterFunctionInvocation *api.FunctionInvocation,
	setterFunctionInvocation *api.FunctionInvocation,
) {
	_, ok := registry[resourceType]
	if !ok {
		registry[resourceType] = make(api.PathToVisitorInfoType)
		for path, pathInfo := range pathInfos {
			registry[resourceType][path] = pathInfo
			setFunctionInvocationsInVisitorPathInfo(pathInfo, getterFunctionInvocation, setterFunctionInvocation)
		}
		return
	}

	// Some paths could already be registered under the same attribute name.
	// Example: resource references that could refer to multiple resource types.
	for path, newPathInfo := range pathInfos {
		oldPathInfo, present := registry[resourceType][path]
		if present {
			if !VisitorInfoEqual(oldPathInfo, newPathInfo, false) {
				slog.Error("info mismatch for path", "path", newPathInfo.Path, "new", newPathInfo, "old", oldPathInfo)
			}
			newPathInfo = oldPathInfo
		} else {
			registry[resourceType][path] = newPathInfo
		}
		setFunctionInvocationsInVisitorPathInfo(newPathInfo, getterFunctionInvocation, setterFunctionInvocation)
	}
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
	getterFunctionInvocation *api.FunctionInvocation,
	setterFunctionInvocation *api.FunctionInvocation,
	normalizePaths bool,
) {
	pathRegistry := resourceProvider.GetPathRegistry()
	_, present := pathRegistry[attributeName]
	if !present {
		pathRegistry[attributeName] = make(api.ResourceTypeToPathToVisitorInfoType)
	}

	attributeRegistry := resourceProvider.GetAttributeRegistry()
	if _, exists := attributeRegistry[attributeName]; !exists {
		attributeRegistry[attributeName] = &api.AttributeDescriptor{
			AttributeName: attributeName,
			AttributeDetails: api.AttributeDetails{
				GetterInvocation: getterFunctionInvocation,
				SetterInvocations: func() []api.FunctionInvocation {
					if setterFunctionInvocation != nil {
						return []api.FunctionInvocation{*setterFunctionInvocation}
					}
					return nil
				}(),
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

	newPathInfos := pathInfos

	// FIXME: Fix or remove normalizePaths
	if normalizePaths {
		newPathInfos = make(api.PathToVisitorInfoType)
		for path, pathInfo := range pathInfos {
			fullyNormalizedPath := normalizePath(resourceType, path, false)
			normalizedPathWithBindings := normalizePath(resourceType, path, true)
			newPathInfo := *pathInfo // deep copy so the path isn't clobbered
			newPathInfo.Path = normalizedPathWithBindings
			newPathInfos[fullyNormalizedPath] = &newPathInfo
		}
	}
	registerPaths(
		pathRegistry[attributeName],
		resourceType,
		newPathInfos,
		getterFunctionInvocation,
		setterFunctionInvocation,
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
func GetPathVisitorInfo(resourceProvider ResourceProvider, resourceType api.ResourceType, path api.UnresolvedPath) *api.PathVisitorInfo {
	normalizedPath := normalizePath(resourceType, path, false)
	// log.Infof("looked up info for resourceType %s path %s\n", resourceType, normalizedPath)

	pathRegistry := resourceProvider.GetPathRegistry()
	for _, resourceTypeToPathToVisitorInfo := range pathRegistry {
		_, present := resourceTypeToPathToVisitorInfo[resourceType]
		if present {
			visitorInfo, found := resourceTypeToPathToVisitorInfo[resourceType][normalizedPath]
			if found {
				return visitorInfo
			}
		}
		// Try wildcard
		_, present = resourceTypeToPathToVisitorInfo[api.ResourceTypeAny]
		if present {
			visitorInfo, found := resourceTypeToPathToVisitorInfo[api.ResourceTypeAny][normalizedPath]
			if found {
				return visitorInfo
			}
		}
	}
	return nil
}

// RegisterNeededPaths marks the specified paths as needed and registers them under their own
// attribute name. These are paths of attributes that generally need to be set based on values
// extracted from other resources or configuration Objects, as opposed to values that would be
// set by default in a configuration object sample, set automatically based on default conventions,
// set by other registered mutation functions, set by other automated processes, or set imperatively
// via functions, UI, other tool, or just by editing the configuration data manually.
func RegisterNeededPaths(
	resourceProvider ResourceProvider,
	resourceType api.ResourceType,
	pathInfos api.PathToVisitorInfoType,
	setterFunctionInvocation *api.FunctionInvocation,
) {
	attributeName := api.AttributeNameNone
	for _, pathInfo := range pathInfos {
		pathInfo.IsNeeded = true
		attributeName = pathInfo.AttributeName
	}
	RegisterPathsByAttributeName(resourceProvider, attributeName, resourceType, pathInfos, nil, setterFunctionInvocation, false)
}

// RegisterProvidedPaths marks the specified paths as provided and registers them under their own
// attribute name. These are paths of attributes that may provide values that could satisfy needed
// values within or across configuration Objects. Provided values are matched with Needed values
// when they have attribute names in common and matching getter (for the Provided value) and setter
// (for the Needed value) function invocation argument values, as disambiguators. The getter and
// setter function names do not need to match, since the provided attributes are expected to be of
// a different kind (different attribute name), from a different resource type.
func RegisterProvidedPaths(
	resourceProvider ResourceProvider,
	resourceType api.ResourceType,
	pathInfos api.PathToVisitorInfoType,
	getterFunctionInvocation *api.FunctionInvocation,
) {
	attributeName := api.AttributeNameNone
	for _, pathInfo := range pathInfos {
		pathInfo.IsProvided = true
		attributeName = pathInfo.AttributeName
	}
	RegisterPathsByAttributeName(resourceProvider, attributeName, resourceType, pathInfos, getterFunctionInvocation, nil, false)
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
				if (needed && info.IsNeeded) || (provided && info.IsProvided) {
					if _, ok := combined[rt]; !ok {
						combined[rt] = make(api.PathToVisitorInfoType)
					}
					existing, exists := combined[rt][path]
					if !exists {
						combined[rt][path] = info
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

// mergeDetails merges getter and setter invocations from src into dst's Details.
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
}
