// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

// Package livestatus defines the contract for live-infrastructure status that a
// client running next to a workload (e.g. argobot) reports back to ConfigHub.
//
// The feedback lands as the confighub.com/live-status annotation on a deployment
// Space, carrying a JSON-encoded Status. It is best-effort and describes observed
// reality, not intent: a missed or stale report costs freshness, never
// correctness. To let such a client find which Space an Argo CD Application maps
// to, ConfigHub stamps the LabelSpaceID / LabelSpaceSlug labels on the
// Application CR it creates for a deployment Space; a client watches Applications
// by those labels and writes Status back to the identified Space.
package livestatus

// Annotation is the well-known deployment-Space annotation key whose value is a
// JSON-encoded Status. Space annotation values are capped at 1024 bytes, so
// Status is intentionally small.
const Annotation = "confighub.com/live-status"

const (
	// LabelSpaceID and LabelSpaceSlug are stamped on the Argo CD Application CR
	// ConfigHub creates for a deployment Space, so a client watching Applications
	// can map one back to its Space. LabelSpaceID is the immutable handle a client
	// keys on when writing Status back; LabelSpaceSlug is the human-readable
	// convenience and can be selected on. Both are valid Kubernetes label values
	// (a UUID and a slug, each within the 63-character limit).
	LabelSpaceID   = "confighub.com/space-id"
	LabelSpaceSlug = "confighub.com/space-slug"
)

// Status is a concise projection of the live state of the infrastructure a
// deployment Space is deployed to. Fields mirror the vocabulary of the reporting
// client (for argobot, an Argo CD Application's sync/health/operation state).
// All string fields keep the raw values the client observed; ConfigHub and the
// UI treat them as opaque display data. Kept small to fit the Space-annotation
// size limit — Message in particular should be truncated by the writer.
type Status struct {
	// Source identifies the reporting client, e.g. "argobot". Required so a reader
	// can tell where the observation came from and, later, distinguish reporters.
	Source string `json:"source"`

	// App is the name of the live object the status was projected from, e.g. the
	// Argo CD Application name. Optional; aids debugging and log lines.
	App string `json:"app,omitempty"`

	// SyncStatus is whether live state matches the desired revision, e.g. Argo's
	// "Synced" / "OutOfSync" / "Unknown".
	SyncStatus string `json:"syncStatus,omitempty"`

	// HealthStatus is the aggregate health, e.g. Argo's "Healthy" / "Progressing"
	// / "Degraded" / "Missing" / "Suspended" / "Unknown".
	HealthStatus string `json:"healthStatus,omitempty"`

	// OperationPhase is the phase of an in-flight operation when one is running,
	// e.g. Argo's "Running" / "Succeeded" / "Failed" / "Error" / "Terminating".
	OperationPhase string `json:"operationPhase,omitempty"`

	// Revision is the revision live state was last synced to (an OCI digest or a
	// git SHA, per the source).
	Revision string `json:"revision,omitempty"`

	// Message is a short human-readable status or error message. The writer should
	// truncate it so the encoded Status stays within the annotation size limit.
	Message string `json:"message,omitempty"`

	// ObservedAt is the RFC 3339 timestamp at which the client made the
	// observation. Required so a reader can tell how fresh the report is.
	ObservedAt string `json:"observedAt"`
}
