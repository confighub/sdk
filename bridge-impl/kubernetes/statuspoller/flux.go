// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package statuspoller

import (
	"context"
	"fmt"

	"github.com/confighub/sdk/core/worker/api"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// FluxKustomization classifies kustomize.toolkit.fluxcd.io/Kustomization.
//
// Signals:
//  1. spec.suspend == true          — definitively stuck (will not reconcile)
//  2. Stalled condition == True     — controller gave up
//  3. Reconciling condition absent AND observedGeneration < generation for > threshold
//     — controller is not acting on the latest spec
func FluxKustomization(ctx context.Context, cc *ClassifierContext, in ClassifierInput) (api.ResourceReadinessType, string) {
	return fluxCommon(ctx, cc, in, "Kustomization")
}

// FluxHelmRelease classifies helm.toolkit.fluxcd.io/HelmRelease. Same shape
// as Kustomization — both honor spec.suspend and expose Stalled/Reconciling
// conditions.
func FluxHelmRelease(ctx context.Context, cc *ClassifierContext, in ClassifierInput) (api.ResourceReadinessType, string) {
	return fluxCommon(ctx, cc, in, "HelmRelease")
}

func fluxCommon(ctx context.Context, cc *ClassifierContext, in ClassifierInput, kindLabel string) (api.ResourceReadinessType, string) {
	if in.Object == nil {
		return "", ""
	}
	// 1. spec.suspend
	if suspended, _, _ := unstructured.NestedBool(in.Object.Object, "spec", "suspend"); suspended {
		return api.ResourceReadinessStuck, fmt.Sprintf("%s is suspended (spec.suspend=true); controller will not reconcile", kindLabel)
	}
	// 2. Stalled condition
	if reason, ok := conditionTrue(in.Object, "Stalled"); ok {
		return api.ResourceReadinessStuck, fmt.Sprintf("%s Stalled: %s", kindLabel, reason)
	}
	// 3. observedGeneration lag with timeout
	if in.Now.Sub(in.LastChangeAt) >= cc.StuckThreshold {
		gen := in.Object.GetGeneration()
		observed, found, err := unstructured.NestedInt64(in.Object.Object, "status", "observedGeneration")
		if err == nil && (!found || observed < gen) {
			return api.ResourceReadinessStuck, fmt.Sprintf("%s controller has not observed generation %d after %s (observedGeneration=%d, found=%v)",
				kindLabel, gen, cc.StuckThreshold, observed, found)
		}
	}
	return "", ""
}
