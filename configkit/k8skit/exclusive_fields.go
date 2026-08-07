// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package k8skit

import (
	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/function/api"
)

// ExclusiveFieldGroup declares a set of sibling fields of which at most one may be present.
//
// Path is the dot-separated path to the object holding them, with "*" for any array index.
// Members are the mutually exclusive fields. Discriminator names the sibling that says which
// member is valid, where the schema has one, and AllowedMember maps each of its values to
// the member it permits — a value with no entry permits none.
//
// This is Kubernetes' patchStrategy:"retainKeys" expressed as data. The API server rejects a
// resource with two members set, so a merge that adds one member and cannot remove the other
// produces configuration that will not apply.
type ExclusiveFieldGroup struct {
	Path          string
	Members       []string
	Discriminator string
	AllowedMember map[string]string
}

// rollingUpdateStrategy is the shape shared by the Deployment, DaemonSet, and StatefulSet
// update strategies: one optional member gated by a `type` discriminator. Recreate and
// OnDelete permit nothing, which is the whole point — a strategy that has moved off
// RollingUpdate must not still carry rollingUpdate.
func rollingUpdateStrategy(path string) ExclusiveFieldGroup {
	return ExclusiveFieldGroup{
		Path:          path,
		Members:       []string{"rollingUpdate"},
		Discriminator: "type",
		AllowedMember: map[string]string{"RollingUpdate": "rollingUpdate"},
	}
}

// volumeSourceMembers are the inline members of a Volume's source union, from
// io.k8s.api.core.v1.Volume: every property but `name`. There is no discriminator — the
// member that is present is what says which kind of volume this is.
var volumeSourceMembers = []string{
	"awsElasticBlockStore", "azureDisk", "azureFile", "cephfs", "cinder", "configMap", "csi",
	"downwardAPI", "emptyDir", "ephemeral", "fc", "flexVolume", "flocker", "gcePersistentDisk",
	"gitRepo", "glusterfs", "hostPath", "image", "iscsi", "nfs", "persistentVolumeClaim",
	"photonPersistentDisk", "portworxVolume", "projected", "quobyte", "rbd", "scaleIO",
	"secret", "storageos", "vsphereVolume",
}

// probeHandlerMembers are the members of a Probe's handler union
// (io.k8s.api.core.v1.Probe): the fields that say *how* to probe, as opposed to the timing
// fields that sit beside them.
var probeHandlerMembers = []string{"exec", "grpc", "httpGet", "tcpSocket"}

// envVarSourceMembers are the members of io.k8s.api.core.v1.EnvVarSource.
var envVarSourceMembers = []string{
	"configMapKeyRef", "fieldRef", "fileKeyRef", "resourceFieldRef", "secretKeyRef",
}

// PodSpecExclusiveFieldGroups are the unions inside a PodSpec, at paths relative to it.
// They are combined with the per-resource-type prefixes in WorkloadMergeKeyFields, the same
// way PodSpecMergeKeyFields is.
var PodSpecExclusiveFieldGroups = []ExclusiveFieldGroup{
	{Path: "volumes.*", Members: volumeSourceMembers},
	// A pod's claim names either a claim or a template, never both.
	{Path: "resourceClaims.*", Members: []string{"resourceClaimName", "resourceClaimTemplateName"}},
}

// containerExclusiveFieldGroups are the unions inside a container, at paths relative to the
// container array. Every container kind has them.
var containerExclusiveFieldGroups = []ExclusiveFieldGroup{
	{Path: "livenessProbe", Members: probeHandlerMembers},
	{Path: "readinessProbe", Members: probeHandlerMembers},
	{Path: "startupProbe", Members: probeHandlerMembers},
	{Path: "lifecycle.postStart", Members: probeHandlerMembers},
	{Path: "lifecycle.preStop", Members: probeHandlerMembers},
	// An env var has a literal value or a reference, never both.
	{Path: "env.*", Members: []string{"value", "valueFrom"}},
	{Path: "env.*.valueFrom", Members: envVarSourceMembers},
}

// containerArrayPaths are the container arrays of a PodSpec, relative to it.
var containerArrayPaths = []string{"containers", "initContainers", "ephemeralContainers"}

// ExclusiveFieldGroups are the unions declared per resource type at absolute paths. The
// PodSpec ones are added for every workload type by buildExclusiveFieldLookup.
var ExclusiveFieldGroups = map[api.ResourceType][]ExclusiveFieldGroup{
	api.ResourceType("apps/v1/Deployment"):  {rollingUpdateStrategy("spec.strategy")},
	api.ResourceType("apps/v1/DaemonSet"):   {rollingUpdateStrategy("spec.updateStrategy")},
	api.ResourceType("apps/v1/StatefulSet"): {rollingUpdateStrategy("spec.updateStrategy")},
	api.ResourceType("v1/PersistentVolume"): {
		{Path: "spec", Members: volumeSourceMembers},
	},
}

// exclusiveFieldLookup maps a resource type to normalized path patterns to their union.
type exclusiveFieldLookup map[api.ResourceType]map[string]yamlkit.ExclusiveFields

func buildExclusiveFieldLookup() exclusiveFieldLookup {
	lookup := make(exclusiveFieldLookup)

	add := func(resourceType api.ResourceType, group ExclusiveFieldGroup) {
		if lookup[resourceType] == nil {
			lookup[resourceType] = make(map[string]yamlkit.ExclusiveFields)
		}
		lookup[resourceType][group.Path] = yamlkit.ExclusiveFields{
			Members:       group.Members,
			Discriminator: group.Discriminator,
			AllowedMember: group.AllowedMember,
		}
	}

	for resourceType, groups := range ExclusiveFieldGroups {
		for _, group := range groups {
			add(resourceType, group)
		}
	}

	for resourceType, prefix := range WorkloadMergeKeyFields {
		for _, group := range PodSpecExclusiveFieldGroups {
			group.Path = prefix + "." + group.Path
			add(resourceType, group)
		}
		for _, containers := range containerArrayPaths {
			for _, group := range containerExclusiveFieldGroups {
				group.Path = prefix + "." + containers + ".*." + group.Path
				add(resourceType, group)
			}
		}
	}

	return lookup
}

// k8sExclusiveFieldLookup is the singleton lookup table, built once at init time.
var k8sExclusiveFieldLookup = buildExclusiveFieldLookup()

// ExclusiveFieldsForPath returns the mutually exclusive sibling fields of the object at the
// given path. The path may use numeric indices or associative segments; both normalize to
// wildcards for lookup, as they do for merge keys.
func (rp *K8sResourceProviderType) ExclusiveFieldsForPath(resourceType api.ResourceType, path string) (yamlkit.ExclusiveFields, bool) {
	normalized := normalizeMergeKeyPath(path)
	if typeMap, ok := k8sExclusiveFieldLookup[resourceType]; ok {
		if fields, ok := typeMap[normalized]; ok {
			return fields, true
		}
	}
	return yamlkit.ExclusiveFields{}, false
}
