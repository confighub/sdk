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
	ResourceCategoryDyanmicData = ResourceCategory("DynamicData")
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

// ResourceInfo contains the ResourceName, ResourceNameWithoutScope, ResourceType, ResourceCategory, and ResourceMergeID for a configuration Element within a configuration Unit.
type ResourceInfo struct {
	ResourceName             ResourceName     `swaggertype:"string" description:"Name of a resource in the system under management represented in the configuration data; Kubernetes resources are represented in the form <metadata.namespace>/<metadata.name>; not all ToolchainTypes necessarily use '/' as a separator between any scope(s) and name or other client-chosen ID"`
	ResourceNameWithoutScope ResourceName     `swaggertype:"string" description:"Name of a resource in the system under management represented in the configuration data, without any uniquifying scope, such as Namespace, Project, Account, Region, etc.; Kubernetes resources are represented in the form <metadata.name>"`
	ResourceNameStableCore   ResourceName     `json:",omitempty" swaggertype:"string" description:"Name of a resource in the system under management represented in the configuration data with generated prefixes and suffixes stripped; empty if nothing to strip"`
	ResourceType             ResourceType     `swaggertype:"string" description:"Type of a resource in the system under management represented in the configuration data; Kubernetes resources are represented in the form <apiVersion>/<kind> (aka group-version-kind)"`
	ResourceCategory         ResourceCategory `json:",omitempty" swaggertype:"string" description:"Category of configuration element represented in the configuration data; Kubernetes and OpenTofu resources are of category Resource, and application configuration files are of category AppConfig"`
	ResourceMergeID          string           `json:",omitempty" swaggertype:"string" description:"Stable identifier (UUID) for a resource stored with the resource data that is intended to remain consistent across resource name and scope changes and across variants, used to match resources between config data documents when computing and patching mutations"`
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

type ResourceTypeAndName string

// TODO: Add ResourceCategory

func ResourceTypeAndNameFromResourceInfo(resourceInfo ResourceInfo) ResourceTypeAndName {
	return ResourceTypeAndName(string(resourceInfo.ResourceType) + "#" + string(resourceInfo.ResourceNameWithoutScope))
}

// ResourceTypeAndFullNameFromResourceInfo returns ResourceType#ResourceName (including namespace/scope)
// Use this for tracking individual resource instances across scopes (e.g., ResourceStatusMap)
// For Kubernetes: returns "apiVersion/kind#namespace/name" (e.g., "apps/v1/Deployment#default/my-app")
func ResourceTypeAndFullNameFromResourceInfo(resourceInfo ResourceInfo) ResourceTypeAndName {
	return ResourceTypeAndName(string(resourceInfo.ResourceType) + "#" + string(resourceInfo.ResourceName))
}
