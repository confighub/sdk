// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package statuspoller

import (
	"context"
	"strings"

	"github.com/confighub/sdk/core/worker/api"
)

// ackGroupSuffix is the group suffix shared by all AWS Controllers for
// Kubernetes (ACK) service controllers, e.g. eks.services.k8s.aws,
// s3.services.k8s.aws, rds.services.k8s.aws.
const ackGroupSuffix = ".services.k8s.aws"

// ACK classifies any AWS Controllers for Kubernetes resource — every CRD whose
// API group ends in ".services.k8s.aws". Registered as a Fallback so it
// applies family-wide without enumerating every ACK CRD.
//
// ACK controllers do not populate status.observedGeneration, so kstatus and
// the Generic / ProgressingTimeout rules cannot infer health from generation
// tracking. ACK communicates state via two well-known conditions:
//
//	ACK.Terminal=True        spec must be edited before any further sync
//	                         (invalid IAM, validation error, "already exists
//	                         not managed by ACK") — controller has stopped
//	ACK.ResourceSynced=True  AWS state matches spec — canonical "in sync"
//
// ACK.ResourceSynced=True alone is sufficient to call the resource Ready;
// most ACK CRDs (S3 Bucket, IAM Role, RDS DBSubnetGroup, …) do not emit a
// Ready condition at all. A few resource kinds that DO emit Ready (e.g. EKS
// Cluster) set ACK.ResourceSynced in lockstep, so the single check covers
// both shapes correctly.
//
// Returns ("", "") for non-ACK groups so the next Fallback (Generic) runs.
func ACK(ctx context.Context, cc *ClassifierContext, in ClassifierInput) (api.ResourceReadinessType, string) {
	if in.Object == nil {
		return "", ""
	}
	if !strings.HasSuffix(in.Object.GroupVersionKind().Group, ackGroupSuffix) {
		return "", ""
	}
	if msg, ok := conditionTrue(in.Object, "ACK.Terminal"); ok {
		return api.ResourceReadinessStuck, "ACK.Terminal: " + msg
	}
	if _, ok := conditionTrue(in.Object, "ACK.ResourceSynced"); ok {
		return api.ResourceReadinessReady, "ACK.ResourceSynced=True"
	}
	return "", ""
}
