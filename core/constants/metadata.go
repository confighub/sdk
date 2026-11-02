// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

// Package constants defines ConfigHub values shared across the server, bots,
// functions, and config kits: the metadata keys below, and API constants
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

// RenderRevision annotation values. render-configmap marks every ConfigMap it renders Latest;
// prune-configmaps marks the older renderings it retains Superseded rather than removing the
// annotation, so that its presence still says the ConfigMap was rendered.
const (
	RenderRevisionLatest     = "Latest"
	RenderRevisionSuperseded = "Superseded"
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

// PackLabel is the label naming the pack a Trigger belongs to: a group of Triggers defined once
// in a policy Space, which a Space or Target opts into by listing the group in its WhereTrigger,
// as in "Space.Slug = 'confighub-system' AND Labels.Pack IN ('basic', 'rbac')".
const PackLabel = "Pack"
