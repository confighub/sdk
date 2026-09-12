// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

// Package constants defines ConfigHub values shared across the server, bridge
// workers, functions, and config kits: the metadata keys below, and API constants
// such as [ServerVersionHeader]. It imports nothing, so the server can use it
// without depending on the API client.
package constants

// Key suffixes for ConfigHub context metadata to inject into managed resources in bridges.
// Paths and Prefixes will depend on the ResourceProvider.
const (
	// Identifying Metadata
	SpaceIDKeySuffix                = "SpaceID"
	UnitSlugKeySuffix               = "UnitSlug"
	RevisionNumKeySuffix            = "RevisionNum"
	ResourceNameStableCoreKeySuffix = "ResourceNameStableCore"

	// Other Metadata
	ConfigMapFormatKeySuffix = "ConfigMapFormat"
	HashKeySuffix            = "Hash"

	// Options
	MutationOptionsKeySuffix = "MutationOptions"
	RenderRevisionKeySuffix  = "RenderRevision"
	VisitorOptionsKeySuffix  = "VisitorOptions"
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
