// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package k8skit

import (
	"strings"

	"github.com/confighub/sdk/core/function/api"
)

// MapKeyPaths lists freeform map paths whose children are dynamic keys (not schema fields).
// During path normalization, child segments of these paths are converted to wildcards.
// Paths use wildcard (*) for array indices and are dot-separated (gaby dot syntax).

// UniversalMapKeyPaths apply to all Kubernetes resource types.
var UniversalMapKeyPaths = []string{
	"metadata.labels.*",
	"metadata.annotations.*",
}

// PodSpecMapKeyPaths are relative to a workload's PodSpec prefix.
var PodSpecMapKeyPaths = []string{
	"containers.*.env.*.valueFrom.fieldRef.fieldPath",
}

// WorkloadMapKeyPaths lists workload-specific map paths beyond the PodSpec.
// These are keyed by resource type.
var WorkloadMapKeyPaths = map[api.ResourceType][]string{
	api.ResourceType("*"): {
		"spec.selector.matchLabels.*",
	},
}

// mapKeyLookup maps resource type to a set of normalized paths that are freeform maps.
type mapKeyLookup map[api.ResourceType]map[string]bool

func buildMapKeyLookup() mapKeyLookup {
	lookup := make(mapKeyLookup)

	addEntry := func(resourceType api.ResourceType, path string) {
		if lookup[resourceType] == nil {
			lookup[resourceType] = make(map[string]bool)
		}
		lookup[resourceType][path] = true
	}

	// Universal paths
	for _, path := range UniversalMapKeyPaths {
		addEntry(api.ResourceType("*"), path)
	}

	// Per-resource-type paths
	for resourceType, paths := range WorkloadMapKeyPaths {
		for _, path := range paths {
			addEntry(resourceType, path)
		}
	}

	// PodSpec-prefixed paths for each workload type, plus template metadata
	for resourceType, prefix := range WorkloadMergeKeyFields {
		for _, path := range PodSpecMapKeyPaths {
			addEntry(resourceType, prefix+"."+path)
		}
		// Template labels and annotations
		templateMetaPrefix := strings.TrimSuffix(prefix, ".spec") + ".metadata"
		addEntry(resourceType, templateMetaPrefix+".labels.*")
		addEntry(resourceType, templateMetaPrefix+".annotations.*")
	}

	return lookup
}

var k8sMapKeyLookup = buildMapKeyLookup()

// IsMapKeyPath returns true if the given path is a freeform map whose children
// are dynamic keys (e.g., label keys, annotation keys) rather than schema-defined
// fields. Child segments of such paths should be wildcarded during normalization.
// The path should end with ".*" to indicate it's asking about map children.
func (rp *K8sResourceProviderType) IsMapKeyPath(resourceType api.ResourceType, path string) bool {
	normalized := normalizeMergeKeyPath(path)

	// Check resource-type-specific entries first
	if typeMap, ok := k8sMapKeyLookup[resourceType]; ok {
		if typeMap[normalized] {
			return true
		}
	}

	// Check universal entries
	if universalMap, ok := k8sMapKeyLookup[api.ResourceType("*")]; ok {
		if universalMap[normalized] {
			return true
		}
	}

	return false
}
