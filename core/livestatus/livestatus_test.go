// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package livestatus_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/confighub/sdk/core/livestatus"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

func TestFromArgoCD(t *testing.T) {
	tests := []struct {
		sync, health, phase string
		want                goclientnew.ReleaseLiveStatus
	}{
		{"Synced", "Healthy", "Succeeded", goclientnew.ReleaseLiveStatus{
			Sync: goclientnew.ReleaseLiveStatusSyncSynced, Health: goclientnew.ReleaseLiveStatusHealthHealthy,
			Operation: goclientnew.Succeeded}},
		{"OutOfSync", "Progressing", "Running", goclientnew.ReleaseLiveStatus{
			Sync: goclientnew.ReleaseLiveStatusSyncOutOfSync, Health: goclientnew.ReleaseLiveStatusHealthProgressing,
			Operation: goclientnew.Running}},
		{"Synced", "Degraded", "Error", goclientnew.ReleaseLiveStatus{
			Sync: goclientnew.ReleaseLiveStatusSyncSynced, Health: goclientnew.ReleaseLiveStatusHealthDegraded,
			Operation: goclientnew.Failed}},
		{"Synced", "Suspended", "Terminating", goclientnew.ReleaseLiveStatus{
			Sync: goclientnew.ReleaseLiveStatusSyncSynced, Health: goclientnew.ReleaseLiveStatusHealthSuspended,
			Operation: goclientnew.Running}},
		// What Argo CD has not decided, or a value it adds later, is Unknown, and no operation is none.
		{"", "", "", goclientnew.ReleaseLiveStatus{
			Sync: goclientnew.ReleaseLiveStatusSyncUnknown, Health: goclientnew.ReleaseLiveStatusHealthUnknown}},
		{"Unknown", "Missing", "", goclientnew.ReleaseLiveStatus{
			Sync: goclientnew.ReleaseLiveStatusSyncUnknown, Health: goclientnew.ReleaseLiveStatusHealthMissing}},
	}
	for _, tt := range tests {
		got := livestatus.FromArgoCD(tt.sync, tt.health, tt.phase)
		tt.want.ReporterSync, tt.want.ReporterHealth, tt.want.ReporterOperation = tt.sync, tt.health, tt.phase
		assert.Equal(t, tt.want, got, "%s/%s/%s", tt.sync, tt.health, tt.phase)
	}
}
