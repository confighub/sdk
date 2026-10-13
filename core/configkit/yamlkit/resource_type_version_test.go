// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package yamlkit

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/third_party/gaby"
)

func TestRemoveSchemaVersionSegments(t *testing.T) {
	for _, tc := range []struct {
		resourceType api.ResourceType
		want         api.ResourceType
	}{
		// A version after what it versions, as registrybot's schemas have it.
		{"registrybot.confighub.com/v1alpha3", "registrybot.confighub.com"},
		{"example.com/myapp/v2", "example.com/myapp"},
		// Abbreviated and other stages.
		{"example.com/v1a1", "example.com"},
		{"example.com/v2b3", "example.com"},
		{"example.com/v1rc2", "example.com"},
		// Wherever the version is.
		{"example.com/v1/Settings", "example.com/Settings"},
		// Nothing to remove.
		{"NoSchema", "NoSchema"},
		{"example.com/myapp", "example.com/myapp"},
		{"example.com/v2app", "example.com/v2app"},
		{"example.com/v1x/Thing", "example.com/v1x/Thing"},
		{"example.com/version1/Thing", "example.com/version1/Thing"},
		// A schema that is nothing but a version is not reduced to nothing.
		{"v1", "v1"},
		{"v1/v2", "v1/v2"},
	} {
		assert.Equal(t, tc.want, RemoveSchemaVersionSegments(tc.resourceType), tc.resourceType)
	}
}

// A map GetVisitorMapForPath builds for a caller's type reaches a resource of that type in another
// API version. A map built from the path registry does not: what is registered for one version is
// not applied to another.
func TestVisitPathsDocMatchesCallerTypeAcrossAPIVersions(t *testing.T) {
	provider := &testResourceProvider{registry: NewResourceProviderRegistry(testToolchain)}
	parsed, err := gaby.ParseAll([]byte(`apiVersion: apps/v1
kind: Deployment
metadata:
  name: app
spec:
  replicas: 2
`))
	require.NoError(t, err)
	visited := func(resourceTypeToPaths api.ResourceTypeToPathToVisitorInfoType) []any {
		var values []any
		_, err := VisitPathsDoc(parsed, resourceTypeToPaths, nil, nil, provider,
			func(_ *gaby.YamlDoc, output any, context VisitorContext, currentDoc *gaby.YamlDoc) (any, error) {
				values = append(values, currentDoc.Data())
				return output, nil
			}, false, nil)
		assert.NoError(t, err)
		return values
	}

	assert.Equal(t, []any{2}, visited(GetVisitorMapForPath(provider, "apps/v1beta1/Deployment", "spec.replicas")))
	assert.Equal(t, []any{2}, visited(GetVisitorMapForPath(provider, "apps/v1/Deployment", "spec.replicas")))
	assert.Empty(t, visited(GetVisitorMapForPath(provider, "apps/v1/StatefulSet", "spec.replicas")))

	registered := api.ResourceTypeToPathToVisitorInfoType{
		"apps/v1beta1/Deployment": {"spec.replicas": &api.PathVisitorInfo{Path: "spec.replicas"}},
	}
	assert.Empty(t, visited(registered))
}
