// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package yamlkit

import (
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/third_party/gaby"
	"github.com/google/uuid"
)

// Normalize iterates over all resources in the parsed configuration data and
// assigns a ResourceMergeID to any resource that doesn't already have one.
// ResourceMergeIDs are random UUIDs stored as context annotations on resources.
// If a resource has a legacy ResourceID but no ResourceMergeID, the legacy
// ResourceID is migrated: its value is written to the new ResourceMergeID path
// and the old ResourceID path is deleted.
// If mutationSources is provided, Normalize will attempt to restore the
// ResourceMergeID from a matching resource in the mutation sources before
// generating a new UUID.
func Normalize(parsedData gaby.Container, resourceProvider ResourceProvider, mutationSources *api.ResourceMutationList) error {
	var msIndex *api.ResourceMutationIndex
	if mutationSources != nil {
		msIndex = api.NewResourceMutationIndex(*mutationSources)
	}

	visitor := func(doc *gaby.YamlDoc, output any, _ int, resourceInfo *api.ResourceInfo) (any, []error) {
		if !IsEmptyOrPlaceHolder(resourceInfo.ResourceMergeID) {
			return output, nil
		}
		// Check whether there is a legacy ResourceID to migrate.
		legacyID, _ := resourceProvider.ResourceIDGetter(doc)
		id := legacyID
		if IsEmptyOrPlaceHolder(id) {
			// Try to restore the ResourceMergeID from mutation sources.
			id = uuid.New().String()
			if msIndex != nil {
				if i, ok := msIndex.Find(*resourceInfo, nil); ok {
					prevID := (*mutationSources)[i].Resource.ResourceMergeID
					if !IsEmptyOrPlaceHolder(prevID) {
						id = prevID
					}
				}
			}
		}
		if err := resourceProvider.SetResourceMergeID(doc, id); err != nil {
			return output, []error{err}
		}
		// Clean up the legacy ResourceID if it was present.
		if legacyID != "" {
			_ = resourceProvider.DeleteResourceID(doc)
		}
		return output, nil
	}
	_, err := VisitResources(parsedData, nil, resourceProvider, visitor)
	return err
}
