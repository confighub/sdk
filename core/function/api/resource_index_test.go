// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package api_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/confighub/sdk/core/function/api"
)

func role(name api.ResourceName) api.ResourceInfo {
	return api.ResourceInfo{ResourceType: "rbac.authorization.k8s.io/v1/Role", ResourceName: name}
}

// A name with its namespace matches the resource with that name, and a name in another namespace
// matches the resource of that name without it, when it is the only one.
func TestResourceIndexMatchesAcrossNamespaces(t *testing.T) {
	index := api.NewResourceIndex(k8s, []api.ResourceInfo{
		role("prod/reader"),
		{ResourceType: "v1/ServiceAccount", ResourceName: "prod/reader"},
	})

	position, found := index.Find(role("prod/reader"))
	assert.True(t, found)
	assert.Equal(t, 0, position)

	position, found = index.Find(role("confighubplaceholder/reader"))
	assert.True(t, found, "a placeholder namespace a variant has filled in")
	assert.Equal(t, 0, position)

	position, found = index.Find(api.ResourceInfo{ResourceType: "rbac.authorization.k8s.io/v1beta1/Role", ResourceName: "staging/reader"})
	assert.True(t, found, "another namespace and another API version")
	assert.Equal(t, 0, position)

	_, found = index.Find(role("prod/writer"))
	assert.False(t, found)
	_, found = index.Find(api.ResourceInfo{ResourceType: "v1/ConfigMap", ResourceName: "prod/reader"})
	assert.False(t, found, "another type")
}

// Two resources of one type and name in different namespaces are two resources: each is found by
// its namespace, and a third namespace finds neither rather than whichever comes first or last.
func TestResourceIndexKeepsNamespacesApart(t *testing.T) {
	index := api.NewResourceIndex(k8s, []api.ResourceInfo{
		role("kube-system/leaderelection"),
		role("cert-manager/leaderelection"),
	})

	position, found := index.Find(role("cert-manager/leaderelection"))
	assert.True(t, found)
	assert.Equal(t, 1, position)
	position, found = index.Find(role("kube-system/leaderelection"))
	assert.True(t, found)
	assert.Equal(t, 0, position)

	match := index.Match(role("confighubplaceholder/leaderelection"))
	assert.False(t, match.Found)
	assert.Equal(t, []api.ResourceName{"cert-manager/leaderelection", "kube-system/leaderelection"}, match.Ambiguous)
}

// Several entries for one resource are one match, chosen by Choose: the last by default.
func TestResourceIndexChoosesAmongEntriesForOneResource(t *testing.T) {
	resources := []api.ResourceInfo{role("prod/reader"), role("prod/reader"), role("prod/writer")}
	index := api.NewResourceIndex(k8s, resources)

	position, found := index.Find(role("prod/reader"))
	assert.True(t, found)
	assert.Equal(t, 1, position)
	position, found = index.Find(role("dev/reader"))
	assert.True(t, found, "entries for one resource are not two resources")
	assert.Equal(t, 1, position)

	index.Choose = func(positions []int) int { return positions[0] }
	position, _ = index.Find(role("prod/reader"))
	assert.Equal(t, 0, position)
}

// An empty name without its scope says nothing about which resource is meant.
func TestResourceIndexIgnoresEmptyNamesWithoutScope(t *testing.T) {
	index := api.NewResourceIndex(nil, []api.ResourceInfo{{ResourceType: "Unit", ResourceName: "space/a"}})
	_, found := index.Find(api.ResourceInfo{ResourceType: "Unit", ResourceName: "space/b"})
	assert.False(t, found)
}

// MutationSources keep the record of two resources of one name in different namespaces apart,
// and still find a resource whose namespace changed.
func TestResourceMutationIndexKeepsNamespacesApart(t *testing.T) {
	mutations := api.ResourceMutationList{
		{Resource: role("kube-system/leaderelection")},
		{Resource: role("cert-manager/leaderelection")},
		{Resource: role("confighubplaceholder/reader")},
	}
	index := api.NewResourceMutationIndex(k8s, mutations)

	position, found := index.Find(role("kube-system/leaderelection"), nil)
	assert.True(t, found)
	assert.Equal(t, 0, position)
	_, found = index.Find(role("default/leaderelection"), nil)
	assert.False(t, found)

	position, found = index.Find(role("prod/reader"), nil)
	assert.True(t, found)
	assert.Equal(t, 2, position)
}
