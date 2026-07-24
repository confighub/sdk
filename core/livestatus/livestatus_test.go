// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package livestatus

import (
	"encoding/json"
	"testing"
)

// TestStatusJSONFieldNames locks the wire contract: the confighub.com/live-status
// value is consumed by argobot (Go writer) and the UI (TypeScript reader), so the
// lowerCamelCase field names must not drift.
func TestStatusJSONFieldNames(t *testing.T) {
	s := Status{
		Source:         "argobot",
		App:            "orders-prod",
		SyncStatus:     "Synced",
		HealthStatus:   "Healthy",
		OperationPhase: "Succeeded",
		Revision:       "sha256:abc",
		Message:        "ok",
		ObservedAt:     "2026-07-24T18:04:11Z",
	}
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	want := []string{"source", "app", "syncStatus", "healthStatus", "operationPhase", "revision", "message", "observedAt"}
	for _, k := range want {
		if _, ok := got[k]; !ok {
			t.Errorf("expected JSON key %q, missing from %s", k, b)
		}
	}
	if len(got) != len(want) {
		t.Errorf("unexpected JSON keys: got %v, want exactly %v", keys(got), want)
	}
}

// TestStatusOmitEmpty confirms only the two required fields are emitted for a
// minimal Status, keeping the annotation small.
func TestStatusOmitEmpty(t *testing.T) {
	b, err := json.Marshal(Status{Source: "argobot", ObservedAt: "2026-07-24T18:04:11Z"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("expected only source and observedAt, got %v", keys(got))
	}
}

func keys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
