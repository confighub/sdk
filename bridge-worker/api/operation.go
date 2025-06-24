// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package api

// TODO unify this API with the function API package.
// This struct is redundant with that of function API.
type ResourceInfo struct {
	ResourceName string `json:",omitempty" description:"Name of a resource in the system under management represented in the configuration data; Kubernetes resources are represented in the form <metadata.namespace>/<metadata.name>"`
	ResourceType string `json:",omitempty" description:"Type of a resource in the system under management represented in the configuration data; Kubernetes resources are represented in the form <apiVersion>/<kind> (aka group-version-kind)"`
}
