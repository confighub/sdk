// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

// Package constants defines shared ConfigHub metadata keys used across
// bridge workers, functions, and config kits.
package constants

// Key suffixes for ConfigHub context metadata to inject into managed resources in bridges.
// Paths and Prefixes will depend on the ResourceProvider.
const (
	// Identifying Metadata
	SpaceIDKeySuffix                = "SpaceID"
	UnitSlugKeySuffix               = "UnitSlug"
	RevisionNumKeySuffix            = "RevisionNum"
	ResourceMergeIDKeySuffix        = "ResourceMergeID"
	ResourceNameStableCoreKeySuffix = "ResourceNameStableCore"

	// Other Metadata
	ConfigMapFormatKeySuffix = "ConfigMapFormat"
	HashKeySuffix            = "Hash"

	// Options
	MutationOptionsKeySuffix = "MutationOptions"
	RenderRevisionKeySuffix  = "RenderRevision"
	VisitorOptionsKeySuffix  = "VisitorOptions"

	// Deprecated: Use ResourceMergeIDKeySuffix instead. Retained for reading legacy data.
	ResourceIDKeySuffix = "ResourceID"
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

const (
	VisitorOptionIgnoreNeeded   = "IgnoreNeeded"
	VisitorOptionIgnoreProvided = "IgnoreProvided"
)

// ConfigMapFormat annotation values for the ConfigMap bridge.
const (
	ConfigMapFormatFile     = "File"
	ConfigMapFormatKeyValue = "KeyValue"
)
