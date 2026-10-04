// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

// Package livestatus maps what a deployment tool reports about a running Release onto the
// normalized values of Release.LiveStatus.
//
// A Release's LiveStatus is written by the client that deploys it -- argobot, for Argo CD -- as a
// patch of the Release, which EditChildren on the Release's Target authorizes. The server reads
// only the normalized Sync, Health and Operation, so a gate means the same whatever the tool; the
// tool's own words are kept beside them for display. A client finds the Release to write by the
// digest its tool reports: for Argo CD, the Application's status.sync.revision is the Release's
// ManifestDigest, and the newest Release of the Space with that ManifestDigest is the one.
package livestatus

import (
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

// FromArgoCD normalizes an Argo CD Application's sync status, health status, and operation phase,
// keeping each value as Argo CD reported it. Reporter, DataSource, Message and ObservedAt are the
// caller's to fill in.
func FromArgoCD(syncStatus, healthStatus, operationPhase string) goclientnew.ReleaseLiveStatus {
	return goclientnew.ReleaseLiveStatus{
		Sync:              argoCDSync(syncStatus),
		Health:            argoCDHealth(healthStatus),
		Operation:         argoCDOperation(operationPhase),
		ReporterSync:      syncStatus,
		ReporterHealth:    healthStatus,
		ReporterOperation: operationPhase,
	}
}

func argoCDSync(status string) goclientnew.ReleaseLiveStatusSync {
	switch status {
	case "Synced":
		return goclientnew.Synced
	case "OutOfSync":
		return goclientnew.OutOfSync
	}
	return goclientnew.Unknown
}

func argoCDHealth(status string) goclientnew.ReleaseLiveStatusHealth {
	switch status {
	case "Healthy":
		return goclientnew.ReleaseLiveStatusHealthHealthy
	case "Progressing":
		return goclientnew.ReleaseLiveStatusHealthProgressing
	case "Degraded":
		return goclientnew.ReleaseLiveStatusHealthDegraded
	case "Suspended":
		return goclientnew.ReleaseLiveStatusHealthSuspended
	case "Missing":
		return goclientnew.ReleaseLiveStatusHealthMissing
	}
	return goclientnew.ReleaseLiveStatusHealthUnknown
}

// argoCDOperation folds Argo CD's five operation phases into three: an operation still under way
// (Running, or Terminating on its way to stopping) is Running, and one that did not complete
// (Failed, or Error) is Failed. No phase means no operation has run.
func argoCDOperation(phase string) goclientnew.ReleaseLiveStatusOperation {
	switch phase {
	case "Running", "Terminating":
		return goclientnew.ReleaseLiveStatusOperationRunning
	case "Succeeded":
		return goclientnew.ReleaseLiveStatusOperationSucceeded
	case "Failed", "Error":
		return goclientnew.ReleaseLiveStatusOperationFailed
	}
	return ""
}
