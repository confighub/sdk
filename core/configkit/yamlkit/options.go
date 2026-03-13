// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package yamlkit

import (
	"github.com/confighub/sdk/constants"
	"github.com/confighub/sdk/function/api"
	"github.com/confighub/sdk/third_party/gaby"
)

// MutationOption values for the confighub.com/MutationOptions annotation (or equivalent
// context path for non-Kubernetes toolchains).
const (
	// MatchByIDOnly instructs ComputeMutations to match this resource only by ResourceID,
	// skipping name-based and fuzzy matching. This is used for immutable resources (e.g.,
	// hash-suffixed ConfigMaps) where each version is a distinct resource that should not
	// be confused with other versions of the same base resource.
	MatchByIDOnly = "MatchByIDOnly"
)

// GetMutationOptions reads the MutationOptions value from a YAML document using the
// resource provider's context path.
func GetMutationOptions(doc *gaby.YamlDoc, resourceProvider ResourceProvider) string {
	optionsPath := resourceProvider.ContextPath(constants.MutationOptionsKeySuffix)
	if optionsPath == "" {
		return ""
	}
	value, found, err := YamlSafePathGetValue[string](doc, api.ResolvedPath(optionsPath), true)
	if err != nil || !found {
		return ""
	}
	return value
}
