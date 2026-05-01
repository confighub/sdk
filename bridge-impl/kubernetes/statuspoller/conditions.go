// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package statuspoller

import "k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

// findCondition walks status.conditions and returns the first entry whose
// type matches condType and whose status matches condStatus. The returned
// string is the entry's formatted "reason: message" (or whichever is non-empty,
// falling back to condType when both are empty).
func findCondition(obj *unstructured.Unstructured, condType, condStatus string) (string, bool) {
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
		if t != condType || s != condStatus {
			continue
		}
		reason, _ := cm["reason"].(string)
		msg, _ := cm["message"].(string)
		switch {
		case reason != "" && msg != "":
			return reason + ": " + msg, true
		case reason != "":
			return reason, true
		case msg != "":
			return msg, true
		default:
			return condType, true
		}
	}
	return "", false
}

// conditionTrue returns the formatted reason/message of the named condition
// if it is present with status=True.
func conditionTrue(obj *unstructured.Unstructured, condType string) (string, bool) {
	return findCondition(obj, condType, "True")
}

// conditionFalse returns the formatted reason/message of the named condition
// if it is present with status=False.
func conditionFalse(obj *unstructured.Unstructured, condType string) (string, bool) {
	return findCondition(obj, condType, "False")
}

// conditionPresent returns the formatted reason/message of the first condition
// of the named type, regardless of its status field. Used for resources whose
// conditions don't follow the standard k8s True/False shape — notably Argo
// Application, whose CRD declares only {type, message, lastTransitionTime} and
// silently strips a status field if you try to set one. For those, the mere
// presence of an "Error"-named condition is the signal.
func conditionPresent(obj *unstructured.Unstructured, condType string) (string, bool) {
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
		if t != condType {
			continue
		}
		reason, _ := cm["reason"].(string)
		msg, _ := cm["message"].(string)
		switch {
		case reason != "" && msg != "":
			return reason + ": " + msg, true
		case reason != "":
			return reason, true
		case msg != "":
			return msg, true
		default:
			return condType, true
		}
	}
	return "", false
}

