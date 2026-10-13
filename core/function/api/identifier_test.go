// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package api_test

import (
	"strings"

	"github.com/confighub/sdk/core/function/api"
)

// kubernetesIdentifier identifies resources as the Kubernetes resource provider does, which this
// module cannot import: names are <namespace>/<name> and types <apiVersion>/<kind>.
type kubernetesIdentifier struct{}

func (kubernetesIdentifier) RemoveScopeFromResourceName(name api.ResourceName) api.ResourceName {
	_, withoutScope, _ := strings.Cut(string(name), "/")
	return api.ResourceName(withoutScope)
}

func (kubernetesIdentifier) RemoveAPIVersionFromResourceType(resourceType api.ResourceType) api.ResourceType {
	segments := strings.Split(string(resourceType), "/")
	switch len(segments) {
	case 2:
		return api.ResourceType(segments[1])
	case 3:
		return api.ResourceType(segments[0] + "/" + segments[2])
	default:
		return resourceType
	}
}

var k8s api.ResourceIdentifier = kubernetesIdentifier{}
