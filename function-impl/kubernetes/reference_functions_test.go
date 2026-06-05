// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package kubernetes

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/confighub/sdk/configkit/k8skit"
	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/function/api"
)

// providesMetadataName reports whether metadata.name is registered as a Provides for
// the given resource type with ProvidedProperties[ResourceType] equal to that type.
// This is what lets a resource of that type satisfy a name reference to it.
func providesMetadataName(resourceType api.ResourceType) bool {
	provided := yamlkit.GetRegisteredProvidedPaths(testResourceProvider)
	pathMap, ok := provided[resourceType]
	if !ok {
		return false
	}
	info, ok := pathMap["metadata.name"]
	if !ok || info.Details == nil {
		return false
	}
	return info.Details.ProvidedProperties["ResourceType"] == string(resourceType)
}

// TestReferenceProvides_CRDTargetsProvideMetadataName verifies that every resource type
// referenced by CRDReferenceFields — both the referring types (map keys) and the targets —
// registers metadata.name as a Provides. Before this was added, references to CRD targets
// that are not kustomize NameReferenceFieldSpecs targets (e.g. Traefik Middleware) could
// never resolve because nothing provided their name.
func TestReferenceProvides_CRDTargetsProvideMetadataName(t *testing.T) {
	targets := map[api.ResourceType]struct{}{}
	for referrer, refs := range k8skit.CRDReferenceFields {
		targets[referrer] = struct{}{}
		for _, ref := range refs {
			targets[ref.Target] = struct{}{}
		}
	}
	for target := range targets {
		assert.True(t, providesMetadataName(target),
			"expected metadata.name to be provided for %s", target)
	}
}

// TestReferenceProvides_MiddlewareUnblocksIngressRoute is the concrete regression: the
// IngressRoute spec.routes.*.middlewares.*.name reference targets a Middleware, and the
// Middleware must provide its own metadata.name for that reference to resolve.
func TestReferenceProvides_MiddlewareUnblocksIngressRoute(t *testing.T) {
	const middleware = api.ResourceType("traefik.io/v1alpha1/Middleware")
	assert.True(t, providesMetadataName(middleware),
		"Middleware must provide metadata.name so IngressRoute middleware references resolve")

	// The reciprocal Needs must share the same resource-type-specific attribute name so
	// the resolver can match them.
	attributeName := attributeNameForResourceType(middleware)
	needed := yamlkit.GetPathRegistryForAttributeName(testResourceProvider, attributeName)
	irPaths, ok := needed[api.ResourceType("traefik.io/v1alpha1/IngressRoute")]
	assert.True(t, ok, "IngressRoute should have needed paths under %s", attributeName)
	info, ok := irPaths["spec.routes.*.middlewares.*.name"]
	assert.True(t, ok, "IngressRoute should need spec.routes.*.middlewares.*.name under %s", attributeName)
	if ok && info.Details != nil {
		assert.Equal(t, string(middleware), info.Details.NeededRequired["ResourceType"])
	}
}

// TestReferenceProvides_BuiltinServiceStillProvides guards against regressing the kustomize
// NameReferenceFieldSpecs path: v1/Service (and other built-ins) must still provide their name.
func TestReferenceProvides_BuiltinServiceStillProvides(t *testing.T) {
	for _, rt := range []api.ResourceType{"v1/Service", "v1/ConfigMap", "v1/Secret", "v1/ServiceAccount"} {
		assert.True(t, providesMetadataName(rt), "expected %s to provide metadata.name", rt)
	}
}
