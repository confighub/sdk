// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package statuspoller

import (
	"context"
	"fmt"
	"time"

	"github.com/confighub/sdk/core/worker/api"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Deployment classifies apps/v1 Deployment.
//
// Signals, in order:
//  1. Progressing condition False with reason ProgressDeadlineExceeded
//     — the Deployment controller itself gave up.
//  2. ReplicaFailure condition True
//     — a hard failure creating replicas.
//  3. Pod-level inspection after a grace period:
//     - ImagePullBackOff / ErrImagePull on any pod container
//     - CrashLoopBackOff with restart count > 0
//     We only inspect pods once LastChangeAt is older than StuckThreshold,
//     so a slow-but-advancing rollout isn't flagged.
//  4. observedGeneration lag fallback (same as Generic).
func Deployment(ctx context.Context, cc *ClassifierContext, in ClassifierInput) (api.ResourceReadinessType, string) {
	if in.Object == nil {
		return "", ""
	}

	// 1. ProgressDeadlineExceeded
	if reason, ok := conditionFalseWithReason(in.Object, "Progressing", "ProgressDeadlineExceeded"); ok {
		return api.ResourceReadinessStuck, "Deployment Progressing=False (ProgressDeadlineExceeded): " + reason
	}

	// 2. ReplicaFailure
	if reason, ok := conditionTrue(in.Object, "ReplicaFailure"); ok {
		return api.ResourceReadinessStuck, "Deployment ReplicaFailure: " + reason
	}

	// 3. Pod-level inspection (only after grace period; only if we have a client).
	if cc.Client != nil && in.Now.Sub(in.LastChangeAt) >= cc.StuckThreshold {
		if reason := firstStuckPodReason(ctx, cc, in.Object); reason != "" {
			return api.ResourceReadinessStuck, reason
		}
	}

	// 4. Generation lag fallback
	return Generic(ctx, cc, in)
}

// StatefulSet classifies apps/v1 StatefulSet. Uses the same pod-level
// inspection as Deployment.
func StatefulSet(ctx context.Context, cc *ClassifierContext, in ClassifierInput) (api.ResourceReadinessType, string) {
	if in.Object == nil {
		return "", ""
	}
	if cc.Client != nil && in.Now.Sub(in.LastChangeAt) >= cc.StuckThreshold {
		if reason := firstStuckPodReason(ctx, cc, in.Object); reason != "" {
			return api.ResourceReadinessStuck, reason
		}
	}
	return Generic(ctx, cc, in)
}

// DaemonSet classifies apps/v1 DaemonSet. Uses the same pod-level inspection
// as Deployment.
func DaemonSet(ctx context.Context, cc *ClassifierContext, in ClassifierInput) (api.ResourceReadinessType, string) {
	if in.Object == nil {
		return "", ""
	}
	if cc.Client != nil && in.Now.Sub(in.LastChangeAt) >= cc.StuckThreshold {
		if reason := firstStuckPodReason(ctx, cc, in.Object); reason != "" {
			return api.ResourceReadinessStuck, reason
		}
	}
	return Generic(ctx, cc, in)
}

// conditionFalseWithReason returns (message, true) if the named condition is
// present with status=False and the given reason.
func conditionFalseWithReason(obj *unstructured.Unstructured, condType, reason string) (string, bool) {
	conds, found, _ := unstructured.NestedSlice(obj.Object, "status", "conditions")
	if !found {
		return "", false
	}
	for _, c := range conds {
		cm, ok := c.(map[string]interface{})
		if !ok {
			continue
		}
		t, _ := cm["type"].(string)
		s, _ := cm["status"].(string)
		r, _ := cm["reason"].(string)
		if t != condType || s != "False" || r != reason {
			continue
		}
		msg, _ := cm["message"].(string)
		return msg, true
	}
	return "", false
}

// firstStuckPodReason lists pods owned by the given workload (Deployment,
// StatefulSet, DaemonSet) via spec.selector.matchLabels and returns a
// human-readable reason for the first container it finds in a stuck state.
// Returns "" when no pod is obviously stuck.
func firstStuckPodReason(ctx context.Context, cc *ClassifierContext, obj *unstructured.Unstructured) string {
	matchLabels, found, _ := unstructured.NestedStringMap(obj.Object, "spec", "selector", "matchLabels")
	if !found || len(matchLabels) == 0 {
		return ""
	}

	fetchCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	pods := &corev1.PodList{}
	err := cc.Client.List(fetchCtx, pods,
		client.InNamespace(obj.GetNamespace()),
		client.MatchingLabelsSelector{Selector: labels.SelectorFromSet(matchLabels)},
	)
	if err != nil {
		// Best effort: on error we don't flag Stuck — we'd rather miss a
		// signal than false-positive.
		return ""
	}

	for i := range pods.Items {
		pod := &pods.Items[i]
		if reason := podStuckReason(pod); reason != "" {
			return fmt.Sprintf("pod %s/%s: %s", pod.Namespace, pod.Name, reason)
		}
	}
	return ""
}

// podStuckReason returns a non-empty string if any of the pod's containers is
// in an obviously stuck waiting state.
func podStuckReason(pod *corev1.Pod) string {
	check := func(statuses []corev1.ContainerStatus) string {
		for _, cs := range statuses {
			if cs.State.Waiting == nil {
				continue
			}
			switch cs.State.Waiting.Reason {
			case "ImagePullBackOff", "ErrImagePull":
				return fmt.Sprintf("container %s: %s (%s)", cs.Name, cs.State.Waiting.Reason, cs.State.Waiting.Message)
			case "CrashLoopBackOff":
				return fmt.Sprintf("container %s: CrashLoopBackOff (restarts=%d): %s",
					cs.Name, cs.RestartCount, cs.State.Waiting.Message)
			case "CreateContainerConfigError", "CreateContainerError":
				return fmt.Sprintf("container %s: %s (%s)", cs.Name, cs.State.Waiting.Reason, cs.State.Waiting.Message)
			}
		}
		return ""
	}
	if r := check(pod.Status.InitContainerStatuses); r != "" {
		return "init " + r
	}
	if r := check(pod.Status.ContainerStatuses); r != "" {
		return r
	}
	return ""
}
