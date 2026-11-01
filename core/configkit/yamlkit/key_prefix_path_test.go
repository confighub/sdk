// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package yamlkit

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/third_party/gaby"
	"github.com/confighub/sdk/core/workerapi"
)

const keyPrefixFixture = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
spec:
  template:
    metadata:
      annotations:
        hash.confighub.com/app-config: confighubplaceholderapp_config
        hash.confighub.com/feature.flags: confighubplaceholderfeature_flags
        confighub.com/Hash: confighubplaceholder
        hashed: "no"
`

const keyPrefixPath = api.UnresolvedPath("spec.template.metadata.annotations.hash~1confighub~1com/*")

func TestIsKeyPrefixSegment(t *testing.T) {
	assert.True(t, IsKeyPrefixSegment("hash.confighub.com/*"))
	assert.True(t, IsKeyPrefixSegment("a*"))
	assert.False(t, IsKeyPrefixSegment("*"), "a bare wildcard matches every key")
	assert.False(t, IsKeyPrefixSegment("*@:name"))
	assert.False(t, IsKeyPrefixSegment("*?name"))
	assert.False(t, IsKeyPrefixSegment("?name=*"), "an associative lookup with a wildcard value")
	assert.False(t, IsKeyPrefixSegment("a*b"))
	assert.False(t, IsKeyPrefixSegment("hash"))
}

func TestResolveKeyPrefixSegment(t *testing.T) {
	docs, err := gaby.ParseAll([]byte(keyPrefixFixture))
	require.NoError(t, err)

	results, err := ResolveAssociativePaths(docs[0], keyPrefixPath, "", false, nil)
	require.NoError(t, err)
	var paths []string
	for _, r := range results {
		paths = append(paths, string(r.Path))
	}
	sort.Strings(paths)
	assert.Equal(t, []string{
		"spec.template.metadata.annotations.hash~1confighub~1com/app-config",
		"spec.template.metadata.annotations.hash~1confighub~1com/feature~1flags",
	}, paths, "only the keys with the prefix, with the dots in each key escaped")

	results, err = ResolveAssociativePaths(docs[0], "spec.template.metadata.labels.hash~1confighub~1com/*", "", false, nil)
	require.NoError(t, err)
	assert.Empty(t, results, "a map that isn't there matches nothing")

	results, err = ResolveAssociativePaths(docs[0], "metadata.name.hash*", "", false, nil)
	require.NoError(t, err)
	assert.Empty(t, results, "a scalar has no keys")
}

func TestNormalizePathKeepsKeyPrefixSegment(t *testing.T) {
	assert.Equal(t, api.UnresolvedPath("spec.template.metadata.annotations.hash.confighub.com/*"),
		normalizePath(nil, "apps/v1/Deployment", keyPrefixPath),
		"registered apart from the other keys of the map, rather than as a wildcard")
	assert.Equal(t, api.UnresolvedPath("spec.template.spec.containers.*.image"),
		normalizePath(nil, "apps/v1/Deployment", "spec.template.spec.containers.?name=web.image"))
}

func TestMatchesKeyPrefixPath(t *testing.T) {
	assert.True(t, matchesKeyPrefixPath(keyPrefixPath, "spec.template.metadata.annotations.hash~1confighub~1com/app-config"))
	assert.True(t, matchesKeyPrefixPath(keyPrefixPath, "spec.template.metadata.annotations.hash~1confighub~1com/feature~1flags"),
		"a key with dots in it is one segment")
	assert.False(t, matchesKeyPrefixPath(keyPrefixPath, "spec.template.metadata.annotations.confighub~1com/Hash"))
	assert.False(t, matchesKeyPrefixPath(keyPrefixPath, "spec.template.metadata.labels.hash~1confighub~1com/app-config"))
	assert.False(t, matchesKeyPrefixPath(keyPrefixPath, "spec.template.metadata.annotations.hash~1confighub~1com/app-config.extra"))
	assert.False(t, matchesKeyPrefixPath("spec.template.metadata.annotations.confighub~1com/Hash",
		"spec.template.metadata.annotations.confighub~1com/Hash"), "a literal registration is found by the exact lookup")
	assert.True(t, matchesKeyPrefixPath("spec.template.spec.containers.*.env.hash*", "spec.template.spec.containers.?name=web.env.hashed"),
		"the segments before the prefix are compared as normalizePath reads them")
}

func TestGetPathVisitorInfoFindsKeyPrefixRegistration(t *testing.T) {
	provider := &testResourceProvider{registry: NewResourceProviderRegistry(workerapi.ToolchainKubernetesYAML)}
	resourceType := api.ResourceType("apps/v1/Deployment")
	attributeName := api.AttributeName("configmap-hash")
	RegisterPathsByAttributeName(provider, attributeName, resourceType, api.PathToVisitorInfoType{
		keyPrefixPath: {Path: keyPrefixPath, AttributeName: attributeName, DataType: api.DataTypeString},
	}, nil, true, false)

	info := GetPathVisitorInfo(provider, resourceType, "spec.template.metadata.annotations.hash~1confighub~1com/app-config")
	require.NotNil(t, info, "a stored key finds the prefix it was registered under")
	assert.Equal(t, attributeName, info.AttributeName)
	assert.Equal(t, keyPrefixPath, info.Path)
	require.NotNil(t, info.Details)
	assert.True(t, info.Details.IsNeeded)

	assert.Nil(t, GetPathVisitorInfo(provider, resourceType, "spec.template.metadata.annotations.hashed"))
	assert.Nil(t, GetPathVisitorInfo(provider, "apps/v1/StatefulSet", "spec.template.metadata.annotations.hash~1confighub~1com/app-config"),
		"registered for one resource type only")

	needed := GetRegisteredNeededPaths(provider)
	docs, err := gaby.ParseAll([]byte(keyPrefixFixture))
	require.NoError(t, err)
	var found []string
	for _, pathInfo := range needed[resourceType] {
		results, err := ResolveAssociativePaths(docs[0], pathInfo.Path, "", false, nil)
		require.NoError(t, err)
		for _, r := range results {
			found = append(found, string(r.Path))
		}
	}
	sort.Strings(found)
	assert.Equal(t, []string{
		"spec.template.metadata.annotations.hash~1confighub~1com/app-config",
		"spec.template.metadata.annotations.hash~1confighub~1com/feature~1flags",
	}, found, "the registered needed path resolves to each key with the prefix")
}
