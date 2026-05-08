// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package statuspoller

import (
	"context"
	"fmt"
	"time"

	"github.com/confighub/sdk/core/worker/api"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Application classifies argoproj.io/Application.
//
// An ArgoCD Application is "stuck" (the controller will not advance it) when:
//
//  1. Auto-sync is off. spec.syncPolicy.automated is either absent or has
//     enabled=false. ArgoCD will not deploy without a manual sync — the exact
//     pattern the ArgoCD renderer uses to hand rendering off to ConfigHub.
//  2. A ComparisonError condition is present. ArgoCD's Application CRD models
//     conditions as {type, message, lastTransitionTime} only — there is no
//     status=True/False field; the apiserver strips one if you try to set it.
//     So presence of the condition (not its truth) is the signal that the
//     server cannot evaluate the application's state against the repo.
//
// Pass-through (returns "", "") otherwise. There is no Generic gen-mismatch
// fallback for Application: the CRD has no status.observedGeneration field,
// so Generic would always return "". A healthy Application with auto-sync on
// and no ComparisonError is treated as Ready via kstatus's Current passthrough
// in Registry.Classify.
// When an ArgoCD Application has auto-sync turned off, the ArgoCD
// controller won't deploy it without manual intervention, so we flag it
// as Stuck. But the ArgoCD renderer flow deliberately turns auto-sync
// off as a signal that ConfigHub (not ArgoCD) will do the deploying,
// and the renderer just needs to read the rendered manifests off the
// resource — usually within a couple of seconds. So we hold the
// auto-sync-disabled verdict for StuckThreshold (~30s): the renderer
// finishes reading well before then and is unaffected, while a real
// long-stuck Application still gets flagged after the grace period.
// ComparisonError stays immediate because that condition is a real
// controller signal that the Application can't be evaluated.
func Application(ctx context.Context, cc *ClassifierContext, in ClassifierInput) (api.ResourceReadinessType, string) {
	st, reason := classifyApplication(in)
	if reason == reasonAutoSyncDisabled && in.Now.Sub(in.LastChangeAt) < cc.StuckThreshold {
		return "", ""
	}
	return st, reason
}

// classifyApplication runs the Application Stuck rules without the
// StuckThreshold gate. ApplicationSet's recursive aggregation reuses this so
// children can be classified Stuck immediately on the first poll, since
// ApplicationSet's own kstate is Current and the wait loop would otherwise
// exit before the gate elapsed.
func classifyApplication(in ClassifierInput) (api.ResourceReadinessType, string) {
	if in.Object == nil {
		return "", ""
	}
	if !hasArgoAutoSync(in.Object) {
		return api.ResourceReadinessStuck, reasonAutoSyncDisabled
	}
	if reason, ok := conditionPresent(in.Object, "ComparisonError"); ok {
		return api.ResourceReadinessStuck, "Application ComparisonError: " + reason
	}
	return "", ""
}

const reasonAutoSyncDisabled = "Application has auto-sync disabled (spec.syncPolicy.automated.enabled=false or absent); controller will not deploy without a manual sync"

// ApplicationSet classifies argoproj.io/ApplicationSet.
//
// An ApplicationSet is stuck when:
//
//  1. ErrorOccurred condition is True. The generator failed (template error,
//     repo clone failure, etc).
//  2. It has generated Applications and every one of them is classified Stuck
//     by the Application classifier. This captures the case where the template
//     sets auto-sync=false on every generated Application: nothing will move
//     until a user intervenes.
//
// Both checks run on every poll. The recursive aggregation used to be gated
// by StuckThreshold, but ApplicationSet's CRD has no observedGeneration and
// no standard Ready/Stalled conditions, so its kstate is Current immediately
// and the bridge applier exits the wait loop on the first poll's Ready
// rollup — the gate could never elapse in production. The aggregation is
// already conservative (returns false unless every non-Synced child is
// Stuck), so running it eagerly is safe; transient mid-generate states with
// any healthy child still return false.
//
// No Generic fallback: the CRD has no status.observedGeneration. A healthy
// ApplicationSet with no ErrorOccurred and at least one non-Stuck child
// passes through to kstatus's Current verdict via Registry.Classify.
func ApplicationSet(ctx context.Context, cc *ClassifierContext, in ClassifierInput) (api.ResourceReadinessType, string) {
	if in.Object == nil {
		return "", ""
	}
	if reason, ok := conditionTrue(in.Object, "ErrorOccurred"); ok {
		return api.ResourceReadinessStuck, "ApplicationSet ErrorOccurred: " + reason
	}
	if cc.Client != nil {
		if stuck, reason := allGeneratedAppsStuck(ctx, cc, in); stuck {
			return api.ResourceReadinessStuck, reason
		}
	}
	return "", ""
}

// hasArgoAutoSync returns true if spec.syncPolicy.automated is present AND
// enabled is not explicitly false. Mirrors the ArgoCD renderer's HasAutoSync.
func hasArgoAutoSync(obj *unstructured.Unstructured) bool {
	automated, found, _ := unstructured.NestedMap(obj.Object, "spec", "syncPolicy", "automated")
	if !found || automated == nil {
		return false
	}
	enabled, hadEnabled, _ := unstructured.NestedBool(obj.Object, "spec", "syncPolicy", "automated", "enabled")
	if hadEnabled && !enabled {
		return false
	}
	return true
}

// allGeneratedAppsStuck lists Applications owned by the ApplicationSet and
// reports Stuck if every one of them is itself Stuck by the Application rules.
// Empty set → not stuck (ApplicationSet may still be generating).
func allGeneratedAppsStuck(ctx context.Context, cc *ClassifierContext, in ClassifierInput) (bool, string) {
	setUID := in.Object.GetUID()
	if setUID == "" {
		return false, ""
	}

	fetchCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	apps := &unstructured.UnstructuredList{}
	apps.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "argoproj.io",
		Version: "v1alpha1",
		Kind:    "ApplicationList",
	})
	if err := cc.Client.List(fetchCtx, apps, client.InNamespace(in.Object.GetNamespace())); err != nil {
		return false, ""
	}

	total := 0
	var firstReason string
	subInput := in
	for i := range apps.Items {
		app := &apps.Items[i]
		if !ownedBy(app, setUID) {
			continue
		}
		total++
		subInput.Object = app
		st, reason := classifyApplication(subInput)
		if st != api.ResourceReadinessStuck {
			return false, ""
		}
		if firstReason == "" {
			firstReason = fmt.Sprintf("%s/%s: %s", app.GetNamespace(), app.GetName(), reason)
		}
	}
	if total == 0 {
		return false, ""
	}
	return true, fmt.Sprintf("ApplicationSet's %d generated Application(s) are all stuck (e.g. %s)", total, firstReason)
}

// ownedBy returns true if obj has any ownerReference with the given UID.
func ownedBy(obj *unstructured.Unstructured, uid types.UID) bool {
	for _, or := range obj.GetOwnerReferences() {
		if or.UID == uid {
			return true
		}
	}
	return false
}
