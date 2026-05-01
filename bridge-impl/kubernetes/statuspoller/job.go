// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package statuspoller

import (
	"context"
	"fmt"

	"github.com/confighub/sdk/core/worker/api"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Job classifies batch/v1 Job.
//
// Signals:
//  1. spec.suspend == true        — controller will not progress pods
//  2. Suspended condition == True — same, observed from status
//  3. status.failed >= backoffLimit — terminal failure (let kstatus Failed pass through; we don't override)
func Job(ctx context.Context, cc *ClassifierContext, in ClassifierInput) (api.ResourceReadinessType, string) {
	if in.Object == nil {
		return "", ""
	}
	if suspended, _, _ := unstructured.NestedBool(in.Object.Object, "spec", "suspend"); suspended {
		return api.ResourceReadinessStuck, "Job is suspended (spec.suspend=true); controller will not start pods"
	}
	if reason, ok := conditionTrue(in.Object, "Suspended"); ok {
		return api.ResourceReadinessStuck, fmt.Sprintf("Job Suspended: %s", reason)
	}
	return "", ""
}
