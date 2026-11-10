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

// Annotations on a validating Trigger that track the enforcement of the policy it checks. A policy
// starts in audit (Warn true), is fixed or excepted until few or no ValidationWarnings are left,
// and is then enforced (Warn false). Where it stands is computed, not stored: a Trigger no Space
// or Target selects has not been rolled out, and one that is selected is in audit or enforced by
// its Warn. These record only what cannot be computed.
const (
	// TriggerPriorityAnnotation is how urgent the policy is: TriggerPriorityHigh, Medium or Low.
	// A Trigger without it has no priority.
	TriggerPriorityAnnotation = "Priority"
	// TriggerEnforcedByAnnotation is when the policy is due to be enforced, in RFC 3339, as
	// every time in the API is written.
	TriggerEnforcedByAnnotation = "EnforcedBy"
	// TriggerEnforcedAtAnnotation is when the Trigger was last changed to enforce, in RFC 3339.
	TriggerEnforcedAtAnnotation = "EnforcedAt"
)

// TriggerPriorityAnnotation values.
const (
	TriggerPriorityHigh   = "High"
	TriggerPriorityMedium = "Medium"
	TriggerPriorityLow    = "Low"
)
