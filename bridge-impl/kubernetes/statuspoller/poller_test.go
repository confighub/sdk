// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package statuspoller

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	pollingevent "sigs.k8s.io/cli-utils/pkg/kstatus/polling/event"
	"sigs.k8s.io/cli-utils/pkg/kstatus/status"
)

// hasProgress distinguishes "controller wrote something" from "same event
// observed again". Correctness of the ProgressingTimeout fallback depends on
// it: if a long-but-progressing install (e.g. a Flux Deployment pulling
// images) never has hasProgress return true, the augmented status will be
// flagged Stuck after 150s even though the controller was advancing status.

func newRawEvent(rv, statusMsg string, st status.Status) *pollingevent.ResourceStatus {
	u := &unstructured.Unstructured{}
	u.SetResourceVersion(rv)
	return &pollingevent.ResourceStatus{
		Status:   st,
		Message:  statusMsg,
		Resource: u,
	}
}

func TestHasProgress_NilPrevIsProgress(t *testing.T) {
	cur := newRawEvent("1", "starting", status.InProgressStatus)
	if !hasProgress(nil, cur) {
		t.Fatalf("nil prev should count as progress")
	}
}

func TestHasProgress_ResourceVersionAdvance(t *testing.T) {
	// The realistic long-provision case: kstatus classification and message
	// stay the same across ticks (Deployment says "InProgress: 0/3 replicas
	// available"), but the underlying resourceVersion advances because the
	// controller is writing status.readyReplicas. hasProgress must detect
	// this so the progressing-timeout clock resets.
	prev := newRawEvent("10", "0/3 replicas available", status.InProgressStatus)
	cur := newRawEvent("11", "0/3 replicas available", status.InProgressStatus)
	if !hasProgress(prev, cur) {
		t.Fatalf("resourceVersion advance must count as progress")
	}
}

func TestHasProgress_MessageChange(t *testing.T) {
	prev := newRawEvent("10", "0/3 replicas available", status.InProgressStatus)
	cur := newRawEvent("10", "1/3 replicas available", status.InProgressStatus)
	if !hasProgress(prev, cur) {
		t.Fatalf("message change must count as progress")
	}
}

func TestHasProgress_StatusFlip(t *testing.T) {
	prev := newRawEvent("10", "msg", status.InProgressStatus)
	cur := newRawEvent("10", "msg", status.CurrentStatus)
	if !hasProgress(prev, cur) {
		t.Fatalf("kstatus flip must count as progress")
	}
}

func TestHasProgress_IdenticalEventsAreNotProgress(t *testing.T) {
	// Same resourceVersion + same status + same message = nothing new. The
	// progressing-timeout clock should NOT reset so a truly silent resource
	// eventually gets flagged Stuck.
	prev := newRawEvent("10", "0/3 replicas available", status.InProgressStatus)
	cur := newRawEvent("10", "0/3 replicas available", status.InProgressStatus)
	if hasProgress(prev, cur) {
		t.Fatalf("identical events must not count as progress")
	}
}

func TestHasProgress_NilResourceDifferences(t *testing.T) {
	withResource := newRawEvent("10", "msg", status.InProgressStatus)
	withoutResource := &pollingevent.ResourceStatus{
		Status:  status.InProgressStatus,
		Message: "msg",
	}
	if !hasProgress(withoutResource, withResource) {
		t.Fatalf("gaining a Resource pointer is progress")
	}
	if !hasProgress(withResource, withoutResource) {
		t.Fatalf("losing a Resource pointer is progress")
	}
	if hasProgress(withoutResource, withoutResource) {
		t.Fatalf("two events with equal nil Resource and equal fields are not progress")
	}
}
