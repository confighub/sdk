// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package statuspoller

import (
	"context"

	"github.com/confighub/sdk/core/worker/api"
)

// CRD classifies apiextensions.k8s.io/CustomResourceDefinition.
//
// Signals:
//  1. NamesAccepted condition False — the API server rejected the schema.
//  2. Established condition False persisting past StuckThreshold.
func CRD(ctx context.Context, cc *ClassifierContext, in ClassifierInput) (api.ResourceReadinessType, string) {
	if in.Object == nil {
		return "", ""
	}
	if reason, ok := conditionFalse(in.Object, "NamesAccepted"); ok {
		return api.ResourceReadinessStuck, "CRD NamesAccepted=False: " + reason
	}
	if in.Now.Sub(in.LastChangeAt) >= cc.StuckThreshold {
		if reason, ok := conditionFalse(in.Object, "Established"); ok {
			return api.ResourceReadinessStuck, "CRD Established=False: " + reason
		}
	}
	return "", ""
}
