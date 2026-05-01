// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package statuspoller

import (
	"testing"
	"time"

	"github.com/confighub/sdk/core/worker/api"
	"sigs.k8s.io/cli-utils/pkg/kstatus/status"
)

func TestACK_ResourceSyncedAloneIsReady(t *testing.T) {
	// Most ACK CRDs (S3 Bucket, IAM Role, RDS DBSubnetGroup, …) emit only
	// ACK.ResourceSynced — no Ready condition. ResourceSynced=True alone
	// must be sufficient to call the resource Ready.
	obj := newObj("s3.services.k8s.aws/v1alpha1", "Bucket", "default", "my-bucket")
	setField(obj, int64(1), "metadata", "generation")
	setCondition(obj, "ACK.ResourceSynced", "True", "", "")

	ctx, cc := ctxCC()
	st, reason := ACK(ctx, cc, mustInput(obj, status.InProgressStatus, 0))
	if st != api.ResourceReadinessReady {
		t.Fatalf("expected Ready, got %q (reason: %q)", st, reason)
	}
	if reason == "" {
		t.Fatalf("expected a non-empty reason")
	}
}

func TestACK_BothConditionsTrueIsReady(t *testing.T) {
	// Realistic ACK EKS Cluster shape: Ready + ACK.ResourceSynced both set.
	// kstatus sees InProgress because observedGeneration is absent.
	obj := newObj("eks.services.k8s.aws/v1alpha1", "Cluster", "default", "confighub-cluster")
	setField(obj, int64(6), "metadata", "generation")
	setCondition(obj, "Ready", "True", "", "")
	setCondition(obj, "ACK.ResourceSynced", "True", "", "")

	ctx, cc := ctxCC()
	st, _ := ACK(ctx, cc, mustInput(obj, status.InProgressStatus, 0))
	if st != api.ResourceReadinessReady {
		t.Fatalf("expected Ready, got %q", st)
	}
}

func TestACK_TerminalIsStuck(t *testing.T) {
	// ACK.Terminal=True is the canonical "controller has given up; spec must
	// be edited" signal — e.g. invalid IAM, validation error, or "already
	// exists not managed by ACK". Must surface as Stuck immediately.
	obj := newObj("eks.services.k8s.aws/v1alpha1", "Cluster", "default", "c")
	setCondition(obj, "ACK.Terminal", "True", "InvalidArgument",
		"The number of replicas per node group must be within 0 and 5")

	ctx, cc := ctxCC()
	st, reason := ACK(ctx, cc, mustInput(obj, status.InProgressStatus, 0))
	if st != api.ResourceReadinessStuck {
		t.Fatalf("expected Stuck, got %q", st)
	}
	if reason == "" {
		t.Fatalf("expected a non-empty reason")
	}
}

func TestACK_TerminalWinsOverResourceSynced(t *testing.T) {
	// If both conditions are present (rare but possible during transitions),
	// Terminal must win — the controller has explicitly stopped.
	obj := newObj("rds.services.k8s.aws/v1alpha1", "DBInstance", "default", "db")
	setCondition(obj, "ACK.ResourceSynced", "True", "", "")
	setCondition(obj, "ACK.Terminal", "True", "InvalidParameterValue", "bad arg")

	ctx, cc := ctxCC()
	st, _ := ACK(ctx, cc, mustInput(obj, status.InProgressStatus, 0))
	if st != api.ResourceReadinessStuck {
		t.Fatalf("expected Stuck (Terminal wins), got %q", st)
	}
}

func TestACK_NoConditionsPassesThrough(t *testing.T) {
	// An ACK resource that hasn't yet set any of the well-known conditions
	// must pass through so the next layer (Generic, ProgressingTimeout) can
	// handle it.
	obj := newObj("eks.services.k8s.aws/v1alpha1", "Cluster", "default", "c")
	ctx, cc := ctxCC()
	st, _ := ACK(ctx, cc, mustInput(obj, status.InProgressStatus, 0))
	if st != "" {
		t.Fatalf("ACK with no conditions must pass through, got %q", st)
	}
}

func TestACK_NonACKGroupIgnored(t *testing.T) {
	// A non-ACK resource that happens to have ACK-shaped condition names
	// must NOT be classified by the ACK rule.
	obj := newObj("apps/v1", "Deployment", "ns", "n")
	setCondition(obj, "ACK.ResourceSynced", "True", "", "")
	setCondition(obj, "ACK.Terminal", "True", "", "")

	ctx, cc := ctxCC()
	st, _ := ACK(ctx, cc, mustInput(obj, status.InProgressStatus, 0))
	if st != "" {
		t.Fatalf("non-ACK group must be ignored; got %q", st)
	}
}

func TestRegistry_ACKReadyOverridesProgressingTimeoutFalsePositive(t *testing.T) {
	// Without the override, an ACK resource that kstatus sees as InProgress
	// (because observedGeneration is absent) would be flagged Stuck by the
	// ProgressingTimeout fallback after 150s. Verify ACK fires first and
	// keeps it Ready.
	obj := newObj("eks.services.k8s.aws/v1alpha1", "Cluster", "default", "confighub-cluster")
	setField(obj, int64(6), "metadata", "generation")
	setCondition(obj, "ACK.ResourceSynced", "True", "", "")

	reg := DefaultRegistry()
	ctx, cc := ctxCCWithProgTimeout()
	st, _ := reg.Classify(ctx, cc, mustInput(obj, status.InProgressStatus, testProgressingTimeout+time.Second))
	if st != api.ResourceReadinessReady {
		t.Fatalf("ACK resource beyond ProgressingTimeout must be Ready via classifier, got %q", st)
	}
}

func TestRegistry_ACKTerminalSurfacesAsStuck(t *testing.T) {
	// ACK.Terminal=True must surface as Stuck via the Registry chain (ACK is
	// registered in Fallbacks and its Stuck verdict is definitive).
	obj := newObj("eks.services.k8s.aws/v1alpha1", "Cluster", "default", "c")
	setCondition(obj, "ACK.Terminal", "True", "InvalidArgument", "bad spec")

	reg := DefaultRegistry()
	ctx, cc := ctxCCWithProgTimeout()
	st, reason := reg.Classify(ctx, cc, mustInput(obj, status.InProgressStatus, 0))
	if st != api.ResourceReadinessStuck {
		t.Fatalf("ACK.Terminal=True must surface as Stuck via Registry, got %q", st)
	}
	if reason == "" {
		t.Fatalf("expected a non-empty reason")
	}
}

func TestRegistry_ACKWithoutConditionsStillStuckByProgressingTimeout(t *testing.T) {
	// An ACK resource that hasn't set any ACK condition yet should still get
	// the ProgressingTimeout fallback — we only suppress the false-Stuck when
	// the controller has positively reported health.
	obj := newObj("eks.services.k8s.aws/v1alpha1", "Cluster", "default", "c")
	reg := DefaultRegistry()
	ctx, cc := ctxCCWithProgTimeout()
	st, _ := reg.Classify(ctx, cc, mustInput(obj, status.InProgressStatus, testProgressingTimeout+time.Second))
	if st != api.ResourceReadinessStuck {
		t.Fatalf("ACK resource without conditions must still hit ProgressingTimeout, got %q", st)
	}
}
