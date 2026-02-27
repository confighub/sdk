// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

// Package constants defines shared ConfigHub metadata keys used across
// bridge workers, functions, and config kits.
package constants

// Key suffixes for ConfigHub context metadata to inject into managed resources in bridges.
// Paths and Prefixes will depend on the ResourceProvider.
const (
	SpaceIDKeySuffix     = "SpaceID"
	UnitSlugKeySuffix    = "UnitSlug"
	RevisionNumKeySuffix = "RevisionNum"
)
