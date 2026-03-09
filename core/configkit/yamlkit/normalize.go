// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package yamlkit

import (
	"github.com/confighub/sdk/function/api"
	"github.com/confighub/sdk/third_party/gaby"
	"github.com/google/uuid"
)

// Normalize iterates over all resources in the parsed configuration data and
// assigns a ResourceID to any resource that doesn't already have one.
// ResourceIDs are random UUIDs stored as context annotations on resources.
func Normalize(parsedData gaby.Container, resourceProvider ResourceProvider) error {
	visitor := func(doc *gaby.YamlDoc, output any, _ int, resourceInfo *api.ResourceInfo) (any, []error) {
		if resourceInfo.ResourceID != "" {
			return output, nil
		}
		id := uuid.New().String()
		if err := resourceProvider.SetResourceID(doc, id); err != nil {
			return output, []error{err}
		}
		return output, nil
	}
	_, err := VisitResources(parsedData, nil, resourceProvider, visitor)
	return err
}
