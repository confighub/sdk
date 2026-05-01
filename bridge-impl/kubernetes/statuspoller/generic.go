// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package statuspoller

import (
	"context"
	"fmt"

	"github.com/confighub/sdk/core/worker/api"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/cli-utils/pkg/kstatus/status"
)

// Generic is the fallback classifier used for kinds without a specific rule.
// It is intentionally conservative: it only returns Stuck when there is a
// positive signal that the controller is not advancing the resource — a
// generation mismatch that has persisted past StuckThreshold.
//
// It never returns a definitive status for KState == Current or Failed (the
// registry short-circuits Failed before any classifier runs, and Current is
// honoured by the registry only after classifiers pass through).
func Generic(ctx context.Context, cc *ClassifierContext, in ClassifierInput) (api.ResourceReadinessType, string) {
	if in.Object == nil || in.KState != status.InProgressStatus {
		return "", ""
	}
	if in.Now.Sub(in.LastChangeAt) < cc.StuckThreshold {
		return "", ""
	}
	gen := in.Object.GetGeneration()
	observed, found, err := unstructured.NestedInt64(in.Object.Object, "status", "observedGeneration")
	if err != nil || !found {
		// No observedGeneration field — we don't know whether the controller
		// is even tracking this resource. Err on "not stuck" to avoid false
		// positives on resources without a standard status contract.
		return "", ""
	}
	if observed >= gen {
		// Controller has acknowledged the latest spec; kstatus says InProgress
		// for other reasons (replicas rolling, etc). Not stuck.
		return "", ""
	}
	return api.ResourceReadinessStuck, fmt.Sprintf("observedGeneration (%d) not yet caught up with generation (%d) after %s",
		observed, gen, cc.StuckThreshold)
}
