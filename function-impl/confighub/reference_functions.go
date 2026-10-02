// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package confighub

import (
	"sort"

	"github.com/confighub/sdk/core/configkit/cubkit"
	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/function/api"
)

// initReferenceFunctions registers the references the resource-type specs declare as needs, and
// the slug of every type they name as a provides, both carrying the ResourceType property that
// matching selects on. This is what get-references reads.
//
// A reference names its referent's Space in its value, as <space>/<slug>, so unlike a Kubernetes
// reference it needs no enricher to state the scope: the name a provider offers is its slug, and a
// qualified name is split by whoever resolves it.
func initReferenceFunctions(rp *cubkit.ConfigHubResourceProviderType) {
	references := cubkit.DeclaredReferences()

	// Every declaring type and every target provides its own slug, so that an entity of any of
	// them can satisfy a reference to it. Sorted, because a path several registrations reach
	// accumulates what they say in the order they arrive.
	providers := map[api.ResourceType]struct{}{}
	for _, reference := range references {
		providers[reference.ResourceType] = struct{}{}
		providers[reference.Target] = struct{}{}
	}
	sortedProviders := make([]api.ResourceType, 0, len(providers))
	for provider := range providers {
		sortedProviders = append(sortedProviders, provider)
	}
	sort.Slice(sortedProviders, func(i, j int) bool { return sortedProviders[i] < sortedProviders[j] })
	namePath := api.UnresolvedPath(cubkit.EntityNamePath)
	for _, provider := range sortedProviders {
		yamlkit.RegisterPathsByAttributeName(rp, api.AttributeNameResourceName, provider, api.PathToVisitorInfoType{
			namePath: {
				Path:          namePath,
				AttributeName: api.AttributeNameResourceName,
				DataType:      api.DataTypeString,
			},
		}, &yamlkit.AttributeRegistrationDetails{
			AttributeNeedsProvidesDetails: api.AttributeNeedsProvidesDetails{
				ProvidedProperties: map[string]string{api.PropertyKeyResourceType: string(provider)},
			},
		}, false, true)
	}

	// ReferencePaths is sorted already. A field names one type, so no path needs to be told
	// apart by a kind beside it, as some Kubernetes references do.
	for _, reference := range references {
		path := api.UnresolvedPath(reference.Path)
		yamlkit.RegisterPathsByAttributeName(rp, reference.AttributeName, reference.ResourceType, api.PathToVisitorInfoType{
			path: {
				Path:          path,
				AttributeName: api.AttributeNameResourceName,
				DataType:      api.DataTypeString,
			},
		}, &yamlkit.AttributeRegistrationDetails{
			AttributeNeedsProvidesDetails: api.AttributeNeedsProvidesDetails{
				NeededRequired: map[string]string{api.PropertyKeyResourceType: string(reference.Target)},
			},
		}, true, false)
	}
}
