// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package yamlkit

import (
	"github.com/confighub/sdk/function/api"
	"github.com/labstack/gommon/log"
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
		AttributeDetailsEqual(pathVisitorInfo1.Info, pathVisitorInfo2.Info, compareFunctions)
}

func setFunctionInvocationsInVisitorPathInfo(
	pathInfo *api.PathVisitorInfo,
	getterFunctionInvocation *api.FunctionInvocation,
	setterFunctionInvocation *api.FunctionInvocation,
) {
	if getterFunctionInvocation == nil && setterFunctionInvocation == nil {
		return
	}
	if pathInfo.Info == nil {
		pathInfo.Info = &api.AttributeDetails{}
	}
	// The getter should be the same.
	if getterFunctionInvocation != nil {
		if pathInfo.Info.GetterInvocation != nil &&
			!FunctionInvocationsEqual(pathInfo.Info.GetterInvocation, getterFunctionInvocation) {
			log.Errorf("different getter function invocations registered: %v vs %v",
				pathInfo.Info.GetterInvocation, getterFunctionInvocation)
		}
		pathInfo.Info.GetterInvocation = getterFunctionInvocation
	}
	if setterFunctionInvocation != nil {
		found := false
		for _, setterInvocation := range pathInfo.Info.SetterInvocations {
			// The function name could be different and/or the argument values could be different.
			// Example: resource references that could refer to multiple resource types.
			if FunctionInvocationsEqual(&setterInvocation, setterFunctionInvocation) {
				found = true
				break
			}
		}
		if !found {
			pathInfo.Info.SetterInvocations = append(pathInfo.Info.SetterInvocations, *setterFunctionInvocation)
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
				log.Errorf("info mismatch for path %s: %v vs %v", newPathInfo.Path, newPathInfo, oldPathInfo)
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
// which is desired when matching all paths to the attribute. api.AttributeNameGeneral is used for
// registrations for general attributes of significance. api.AttributeNameNeededValue is used for
// needed values. api.AttributeNameProvidedValue is used for provided values. AttributeNameResourceName
// is used for references to resource names. Other attribute names are used for specific setters and/or
// getters, especially for attributes that appear in multiple resource types and/or locations.
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
	newPathInfos := pathInfos
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
// to a visitor function.
func GetPathRegistryForAttributeName(
	resourceProvider ResourceProvider,
	attributeName api.AttributeName,
) api.ResourceTypeToPathToVisitorInfoType {
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

// GetPathVisitorInfo returns the path visitor specification for the specified path within the
// specified resource type to pass to a visitor function.
func GetPathVisitorInfo(resourceProvider ResourceProvider, resourceType api.ResourceType, path api.UnresolvedPath) *api.PathVisitorInfo {
	normalizedPath := normalizePath(resourceType, path, false)
	// log.Infof("looked up info for resourceType %s path %s\n", resourceType, normalizedPath)

	var visitorInfo *api.PathVisitorInfo
	var resourceTypeToPathToVisitorInfo api.ResourceTypeToPathToVisitorInfoType
	var present bool
	pathRegistry := resourceProvider.GetPathRegistry()
	resourceTypeToPathToVisitorInfo, present = pathRegistry[api.AttributeNameGeneral]
	if !present {
		// This shouldn't happen
		log.Error("no general attribute path registry")
		return nil
	}
	_, present = resourceTypeToPathToVisitorInfo[resourceType]
	if present {
		visitorInfo, present = resourceTypeToPathToVisitorInfo[resourceType][normalizedPath]
	}
	if !present {
		// Try wildcard
		_, present = resourceTypeToPathToVisitorInfo[api.ResourceTypeAny]
		if present {
			visitorInfo, present = resourceTypeToPathToVisitorInfo[api.ResourceTypeAny][normalizedPath]
		}
	}
	if !present {
		return nil
	}
	if visitorInfo.AttributeName == api.AttributeNameNone {
		log.Debugf("path %s registered with no AttributeName", normalizedPath)
		visitorInfo.AttributeName = api.AttributeNameGeneral
	}
	// log.Infof("found info for resourceType %s path %s\n", resourceType, normalizedPath)
	return visitorInfo
}

// RegisterNeededPaths registers paths in the api.AttributeNameNeededValue path registry.
// These are paths of attributes that generally need to be set based on values extracted from
// other resources or configuration Objects, as opposed to values that would be set by default
// in a configuration object sample, set automatically based on default conventions, set
// by other registered mutation functions, set by other automated processes, or set imperatively
// via functions, UI, other tool, or just by editing the configuration data manually.
func RegisterNeededPaths(
	resourceProvider ResourceProvider,
	resourceType api.ResourceType,
	pathInfos api.PathToVisitorInfoType,
	setterFunctionInvocation *api.FunctionInvocation,
) {
	RegisterPathsByAttributeName(resourceProvider, api.AttributeNameNeededValue, resourceType, pathInfos, nil, setterFunctionInvocation, false)
}

// RegisterProvidedPaths registers paths in the api.AttributeNameProvidedValue path registry.
// These are paths of attributes that may provide values that could satisfy needed values within
// or across configuration Objects. Provided values are matched with Needed values when they have
// attribute names in common and matching getter (for the Provided value) and setter (for the Needed value)
// function invocation argument values, as disambiguators. The getter and setter function names do not
// need to match, since the provided attributes are expected to be of a different kind (different attribute
// name), from a different resource type.
func RegisterProvidedPaths(
	resourceProvider ResourceProvider,
	resourceType api.ResourceType,
	pathInfos api.PathToVisitorInfoType,
	getterFunctionInvocation *api.FunctionInvocation,
) {
	RegisterPathsByAttributeName(resourceProvider, api.AttributeNameProvidedValue, resourceType, pathInfos, getterFunctionInvocation, nil, false)
}
