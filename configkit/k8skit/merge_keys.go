// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package k8skit

import (
	"regexp"
	"strings"

	"github.com/confighub/sdk/core/function/api"
)

// MergeKeyField describes a strategic merge patch key for an array field.
// Path is the dot-separated path to the array field (gaby dot syntax).
// Wildcards (*) represent any array index within the path.
// Key is the field name within array items used as the merge key.
//
// ExtraKeys names the remaining fields of a composite key, for the arrays Kubernetes
// declares with more than one x-kubernetes-list-map-key: a container port is identified by
// its number and its protocol, and a topology spread constraint by its topology key and
// its whenUnsatisfiable. Two elements that agree on Key alone but differ in an ExtraKey
// are different elements, and matching them would merge one into the other.
type MergeKeyField struct {
	Path      string
	Key       string
	ExtraKeys []string
}

// Keys returns the full key list, Key first.
func (f MergeKeyField) Keys() []string {
	if len(f.ExtraKeys) == 0 {
		return []string{f.Key}
	}
	return append([]string{f.Key}, f.ExtraKeys...)
}

// StrategicMergeKeyFields maps resource types to their strategic merge patch key fields,
// extracted from x-kubernetes-patch-merge-key in the Kubernetes swagger spec.
// Status fields are excluded. ResourceType "*" contains keys common to all resources.
//
// Pod spec merge keys that are shared across workload types are listed under
// PodSpecMergeKeyFields and PodTemplateSpecMergeKeyFields with their relative
// paths; they are combined with per-resource-type prefixes in
// WorkloadMergeKeyFields.
var StrategicMergeKeyFields = map[api.ResourceType][]MergeKeyField{
	// Universal: present in all resources
	api.ResourceType("*"): {
		{Path: "metadata.ownerReferences", Key: "uid"},
	},

	// -------------------------------------------------------------------------
	// Admission registration
	// -------------------------------------------------------------------------
	api.ResourceType("admissionregistration.k8s.io/v1/MutatingAdmissionPolicy"): {
		{Path: "spec.matchConditions", Key: "name"},
	},
	api.ResourceType("admissionregistration.k8s.io/v1/ValidatingAdmissionPolicy"): {
		{Path: "spec.matchConditions", Key: "name"},
		{Path: "spec.variables", Key: "name"},
	},
	api.ResourceType("admissionregistration.k8s.io/v1/MutatingWebhookConfiguration"): {
		{Path: "webhooks", Key: "name"},
		{Path: "webhooks.*.matchConditions", Key: "name"},
	},
	api.ResourceType("admissionregistration.k8s.io/v1/ValidatingWebhookConfiguration"): {
		{Path: "webhooks", Key: "name"},
		{Path: "webhooks.*.matchConditions", Key: "name"},
	},

	// -------------------------------------------------------------------------
	// CRDs
	// -------------------------------------------------------------------------
	api.ResourceType("apiextensions.k8s.io/v1/CustomResourceDefinition"): {
		{Path: "spec.versions.*.schema.openAPIV3Schema.x-kubernetes-validations", Key: "rule"},
	},

	// -------------------------------------------------------------------------
	// Services
	// -------------------------------------------------------------------------
	api.ResourceType("v1/Service"): {
		{Path: "spec.ports", Key: "port", ExtraKeys: []string{"protocol"}},
	},

	// -------------------------------------------------------------------------
	// ServiceAccount
	// -------------------------------------------------------------------------
	api.ResourceType("v1/ServiceAccount"): {
		{Path: "secrets", Key: "name"},
	},

	// -------------------------------------------------------------------------
	// CSINode
	// -------------------------------------------------------------------------
	api.ResourceType("storage.k8s.io/v1/CSINode"): {
		{Path: "spec.drivers", Key: "name"},
	},

	// -------------------------------------------------------------------------
	// ComponentStatus (deprecated but still in spec)
	// -------------------------------------------------------------------------
	api.ResourceType("v1/ComponentStatus"): {
		{Path: "conditions", Key: "type"},
	},

	// -------------------------------------------------------------------------
	// StatefulSet-specific (in addition to pod template merge keys)
	// -------------------------------------------------------------------------
	api.ResourceType("apps/v1/StatefulSet"): {
		{Path: "spec.volumeClaimTemplates.*.metadata.ownerReferences", Key: "uid"},
	},
}

// PodSpecMergeKeyFields contains the strategic merge key fields within a PodSpec.
// These are relative paths from the PodSpec root.
var PodSpecMergeKeyFields = []MergeKeyField{
	{Path: "containers", Key: "name"},
	{Path: "containers.*.env", Key: "name"},
	{Path: "containers.*.ports", Key: "containerPort", ExtraKeys: []string{"protocol"}},
	{Path: "containers.*.volumeDevices", Key: "devicePath"},
	{Path: "containers.*.volumeMounts", Key: "mountPath"},
	{Path: "ephemeralContainers", Key: "name"},
	{Path: "ephemeralContainers.*.env", Key: "name"},
	{Path: "ephemeralContainers.*.ports", Key: "containerPort", ExtraKeys: []string{"protocol"}},
	{Path: "ephemeralContainers.*.volumeDevices", Key: "devicePath"},
	{Path: "ephemeralContainers.*.volumeMounts", Key: "mountPath"},
	{Path: "hostAliases", Key: "ip"},
	{Path: "imagePullSecrets", Key: "name"},
	{Path: "initContainers", Key: "name"},
	{Path: "initContainers.*.env", Key: "name"},
	{Path: "initContainers.*.ports", Key: "containerPort", ExtraKeys: []string{"protocol"}},
	{Path: "initContainers.*.volumeDevices", Key: "devicePath"},
	{Path: "initContainers.*.volumeMounts", Key: "mountPath"},
	{Path: "resourceClaims", Key: "name"},
	{Path: "schedulingGates", Key: "name"},
	{Path: "topologySpreadConstraints", Key: "topologyKey", ExtraKeys: []string{"whenUnsatisfiable"}},
	{Path: "volumes", Key: "name"},
}

// WorkloadMergeKeyFields maps workload resource types to the prefix path for their
// embedded PodSpec. The full merge key paths are formed by joining the prefix with
// each PodSpecMergeKeyFields entry's Path.
var WorkloadMergeKeyFields = map[api.ResourceType]string{
	api.ResourceType("v1/Pod"):                       "spec",
	api.ResourceType("apps/v1/Deployment"):           "spec.template.spec",
	api.ResourceType("apps/v1/ReplicaSet"):           "spec.template.spec",
	api.ResourceType("apps/v1/DaemonSet"):            "spec.template.spec",
	api.ResourceType("apps/v1/StatefulSet"):          "spec.template.spec",
	api.ResourceType("batch/v1/Job"):                 "spec.template.spec",
	api.ResourceType("batch/v1/CronJob"):             "spec.jobTemplate.spec.template.spec",
	api.ResourceType("v1/ReplicationController"):     "spec.template.spec",
	api.ResourceType("v1/PodTemplate"):               "template.spec",
	api.ResourceType("argoproj.io/v1alpha1/Rollout"): "spec.template.spec",
}

// mergeKeyLookup maps a resource type to a map of normalized path patterns to merge keys.
// Path patterns have numeric indices replaced with * for matching.
type mergeKeyLookup map[api.ResourceType]map[string][]string

// numericSegmentRegexp matches one or more digits (a numeric array index).
var numericSegmentRegexp = regexp.MustCompile(`^[0-9]+$`)

// normalizeMergeKeyPath replaces numeric array indices and associative segments
// in a dot-separated path with "*" for merge key lookup matching.
func normalizeMergeKeyPath(path string) string {
	segments := strings.Split(path, ".")
	for i, seg := range segments {
		if numericSegmentRegexp.MatchString(seg) || strings.HasPrefix(seg, "?") {
			segments[i] = "*"
		}
	}
	return strings.Join(segments, ".")
}

// buildMergeKeyLookup constructs the merge key lookup table from the static
// StrategicMergeKeyFields, PodSpecMergeKeyFields, and WorkloadMergeKeyFields data.
func buildMergeKeyLookup() mergeKeyLookup {
	lookup := make(mergeKeyLookup)

	addEntry := func(resourceType api.ResourceType, path string, keys []string) {
		if lookup[resourceType] == nil {
			lookup[resourceType] = make(map[string][]string)
		}
		lookup[resourceType][path] = keys
	}

	// Add per-resource-type fields (including universal "*")
	for resourceType, fields := range StrategicMergeKeyFields {
		for _, field := range fields {
			addEntry(resourceType, field.Path, field.Keys())
		}
	}

	// Add PodSpec fields for each workload type with their prefix
	for resourceType, prefix := range WorkloadMergeKeyFields {
		for _, field := range PodSpecMergeKeyFields {
			fullPath := prefix + "." + field.Path
			addEntry(resourceType, fullPath, field.Keys())
		}
	}

	return lookup
}

// k8sMergeKeyLookup is the singleton lookup table, built once at init time.
var k8sMergeKeyLookup = buildMergeKeyLookup()

// MergeKeysForPath returns the merge key field names for the given K8s resource type
// and array path. The path may use numeric indices or wildcards; numeric indices
// are normalized to wildcards for lookup. Returns (nil, false) if no merge key
// is defined for the path.
func (rp *K8sResourceProviderType) MergeKeysForPath(resourceType api.ResourceType, path string) ([]string, bool) {
	normalized := normalizeMergeKeyPath(path)

	// Check resource-type-specific entries first
	if typeMap, ok := k8sMergeKeyLookup[resourceType]; ok {
		if keys, ok := typeMap[normalized]; ok {
			return keys, true
		}
	}

	// Check universal entries (ResourceType "*")
	if universalMap, ok := k8sMergeKeyLookup[api.ResourceType("*")]; ok {
		if keys, ok := universalMap[normalized]; ok {
			return keys, true
		}
	}

	return nil, false
}
