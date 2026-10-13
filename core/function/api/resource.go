// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package api

// ResourceCategory represents the category of syntactic construct represented.
// Each configuration toolchain defines its own set of valid resource categories,
// but there are some common categories, such as Resource and AppConfig.
type ResourceCategory string

const (
	ResourceCategoryInvalid     = ResourceCategory("Invalid")
	ResourceCategoryResource    = ResourceCategory("Resource")
	ResourceCategoryDynamicData = ResourceCategory("DynamicData")
	ResourceCategoryAppConfig   = ResourceCategory("AppConfig")
)

// ResourceType represents a fully qualified resource type of a resource provider.
// Each resource provider defines its own set of valid resource types.
type ResourceType string

const (
	ResourceTypeAny = ResourceType("*")
)

const MaxResourceTypeLength = 128
const MaxResourceCategoryLength = MaxResourceTypeLength
const MaxResourceNameLength = 256

// ResourceCategoryType is a tuple containing the ResourceCategory and ResourceType.
type ResourceCategoryType struct {
	ResourceCategory ResourceCategory
	ResourceType     ResourceType
}

// ResourceName represents a fully qualified resource name of a resource for a specific resource provider.
type ResourceName string

// ResourceInfo contains the ResourceName, ResourceNameWithoutScope, ResourceType, and ResourceCategory for a configuration Element within a configuration Unit.
type ResourceInfo struct {
	ResourceName             ResourceName     `swaggertype:"string" description:"Name of a resource in the system under management represented in the configuration data; Kubernetes resources are represented in the form <metadata.namespace>/<metadata.name>; not all ToolchainTypes necessarily use '/' as a separator between any scope(s) and name or other client-chosen ID"`
	ResourceNameWithoutScope ResourceName     `swaggertype:"string" description:"Name of a resource in the system under management represented in the configuration data, without any uniquifying scope, such as Namespace, Project, Account, Region, etc.; Kubernetes resources are represented in the form <metadata.name>"`
	ResourceNameStableCore   ResourceName     `json:",omitempty" swaggertype:"string" description:"Name of a resource in the system under management represented in the configuration data with generated prefixes and suffixes stripped; empty if nothing to strip"`
	ResourceType             ResourceType     `swaggertype:"string" description:"Type of a resource in the system under management represented in the configuration data; Kubernetes resources are represented in the form <apiVersion>/<kind> (aka group-version-kind)"`
	ResourceCategory         ResourceCategory `json:",omitempty" swaggertype:"string" description:"Category of configuration element represented in the configuration data; Kubernetes resources are of category Resource, and application configuration files are of category AppConfig"`
}
type ResourceInfoList []ResourceInfo

func EffectiveResourceName(ri *ResourceInfo) ResourceName {
	if ri.ResourceNameStableCore != "" {
		return ri.ResourceNameStableCore
	}
	return ri.ResourceName
}

// Resource contains the ResourceName, ResourceType, ResourceCategory, and Body for a configuration Element within a configuration Unit.
type Resource struct {
	ResourceInfo
	ResourceBody string `json:",omitempty" description:"Full configuration data of the resource"`
}
type ResourceList []Resource

// ResourceIdentifier reduces a resource's type and name to the parts that identify the resource,
// in the toolchain-specific way only a resource provider knows. Every resource provider is one.
//
// A resource's identity must survive changes that do not make it a different resource: the
// scope it is placed in, which clones and variants change, and the API version its type is
// written in, which an upgrade changes (a SecretStore moving from external-secrets.io/v1beta1
// to v1 is the same SecretStore). ResourceType itself keeps the version, because what the
// resource's fields mean depends on it.
type ResourceIdentifier interface {
	// RemoveScopeFromResourceName returns the name without the scope, such as a namespace,
	// that qualifies it.
	RemoveScopeFromResourceName(resourceName ResourceName) ResourceName
	// RemoveAPIVersionFromResourceType returns the type without the API version, if the type
	// carries one.
	RemoveAPIVersionFromResourceType(resourceType ResourceType) ResourceType
}

// ResourceTypeWithoutAPIVersion returns the part of a resource type that identifies the type
// across API versions. A nil identifier, for a toolchain no resource provider is registered for,
// leaves the type as it is.
func ResourceTypeWithoutAPIVersion(identifier ResourceIdentifier, resourceType ResourceType) ResourceType {
	if identifier == nil {
		return resourceType
	}
	return identifier.RemoveAPIVersionFromResourceType(resourceType)
}

// SameResourceType reports whether two resource types are the same type, possibly written in
// different API versions. Use it, not ==, wherever a type identifies a resource.
func SameResourceType(identifier ResourceIdentifier, resourceTypeA, resourceTypeB ResourceType) bool {
	return resourceTypeA == resourceTypeB ||
		ResourceTypeWithoutAPIVersion(identifier, resourceTypeA) == ResourceTypeWithoutAPIVersion(identifier, resourceTypeB)
}

// ResourceNameWithoutScopeFromResourceInfo returns ResourceNameWithoutScope, deriving it from
// ResourceName for a ResourceInfo recorded without it.
func ResourceNameWithoutScopeFromResourceInfo(identifier ResourceIdentifier, resourceInfo ResourceInfo) ResourceName {
	if resourceInfo.ResourceNameWithoutScope != "" || identifier == nil {
		return resourceInfo.ResourceNameWithoutScope
	}
	return identifier.RemoveScopeFromResourceName(resourceInfo.ResourceName)
}

// ResourceTypeAndName identifies a resource in a key built by ResourceTypeAndNameFromResourceInfo
// or ResourceTypeAndFullNameFromResourceInfo. It is never parsed back into a type and a name.
type ResourceTypeAndName string

// TODO: Add ResourceCategory

// ResourceTypeAndNameFromResourceInfo returns the key identifying a resource within a Unit:
// its type without the API version, and its name without the scope. For Kubernetes, the key of
// apps/v1/Deployment default/my-app is "apps/Deployment#my-app".
func ResourceTypeAndNameFromResourceInfo(identifier ResourceIdentifier, resourceInfo ResourceInfo) ResourceTypeAndName {
	return ResourceTypeAndName(string(ResourceTypeWithoutAPIVersion(identifier, resourceInfo.ResourceType)) + "#" +
		string(ResourceNameWithoutScopeFromResourceInfo(identifier, resourceInfo)))
}

// ResourceTypeAndFullNameFromResourceInfo returns the key identifying a resource including its
// scope, for tracking individual resource instances across scopes: its type without the API
// version, and its full name. For Kubernetes, the key of apps/v1/Deployment default/my-app is
// "apps/Deployment#default/my-app".
func ResourceTypeAndFullNameFromResourceInfo(identifier ResourceIdentifier, resourceInfo ResourceInfo) ResourceTypeAndName {
	return ResourceTypeAndName(string(ResourceTypeWithoutAPIVersion(identifier, resourceInfo.ResourceType)) + "#" +
		string(resourceInfo.ResourceName))
}

// SameResource reports whether two ResourceInfos identify the same resource, comparing their
// types without API versions and their names without scopes.
func SameResource(identifier ResourceIdentifier, resourceInfoA, resourceInfoB ResourceInfo) bool {
	return ResourceTypeAndNameFromResourceInfo(identifier, resourceInfoA) == ResourceTypeAndNameFromResourceInfo(identifier, resourceInfoB)
}
