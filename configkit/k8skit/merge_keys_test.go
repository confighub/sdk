// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package k8skit

import (
	"testing"

	"github.com/confighub/sdk/core/function/api"
	"github.com/stretchr/testify/assert"
)

func TestMergeKeyForPath(t *testing.T) {
	rp := NewK8sResourceProvider()

	tests := []struct {
		name         string
		resourceType api.ResourceType
		path         string
		wantKey      string
		wantFound    bool
	}{
		// Deployment pod spec fields
		{
			name:         "Deployment containers",
			resourceType: "apps/v1/Deployment",
			path:         "spec.template.spec.containers",
			wantKey:      "name",
			wantFound:    true,
		},
		{
			name:         "Deployment containers with numeric index in parent",
			resourceType: "apps/v1/Deployment",
			path:         "spec.template.spec.containers.0.env",
			wantKey:      "name",
			wantFound:    true,
		},
		{
			name:         "Deployment containers with numeric index - ports",
			resourceType: "apps/v1/Deployment",
			path:         "spec.template.spec.containers.0.ports",
			wantKey:      "containerPort",
			wantFound:    true,
		},
		{
			name:         "Deployment containers with numeric index - volumeMounts",
			resourceType: "apps/v1/Deployment",
			path:         "spec.template.spec.containers.2.volumeMounts",
			wantKey:      "mountPath",
			wantFound:    true,
		},
		{
			name:         "Deployment volumes",
			resourceType: "apps/v1/Deployment",
			path:         "spec.template.spec.volumes",
			wantKey:      "name",
			wantFound:    true,
		},
		{
			name:         "Deployment initContainers",
			resourceType: "apps/v1/Deployment",
			path:         "spec.template.spec.initContainers",
			wantKey:      "name",
			wantFound:    true,
		},
		{
			name:         "Deployment initContainers env",
			resourceType: "apps/v1/Deployment",
			path:         "spec.template.spec.initContainers.0.env",
			wantKey:      "name",
			wantFound:    true,
		},

		// Pod (different prefix)
		{
			name:         "Pod containers",
			resourceType: "v1/Pod",
			path:         "spec.containers",
			wantKey:      "name",
			wantFound:    true,
		},
		{
			name:         "Pod containers env",
			resourceType: "v1/Pod",
			path:         "spec.containers.0.env",
			wantKey:      "name",
			wantFound:    true,
		},

		// CronJob (deeper prefix)
		{
			name:         "CronJob containers",
			resourceType: "batch/v1/CronJob",
			path:         "spec.jobTemplate.spec.template.spec.containers",
			wantKey:      "name",
			wantFound:    true,
		},

		// Service
		{
			name:         "Service ports",
			resourceType: "v1/Service",
			path:         "spec.ports",
			wantKey:      "port",
			wantFound:    true,
		},

		// Universal
		{
			name:         "Universal ownerReferences on Deployment",
			resourceType: "apps/v1/Deployment",
			path:         "metadata.ownerReferences",
			wantKey:      "uid",
			wantFound:    true,
		},
		{
			name:         "Universal ownerReferences on Service",
			resourceType: "v1/Service",
			path:         "metadata.ownerReferences",
			wantKey:      "uid",
			wantFound:    true,
		},

		// Type-specific
		{
			name:         "StatefulSet volumeClaimTemplates ownerReferences",
			resourceType: "apps/v1/StatefulSet",
			path:         "spec.volumeClaimTemplates.0.metadata.ownerReferences",
			wantKey:      "uid",
			wantFound:    true,
		},
		{
			name:         "MutatingWebhookConfiguration webhooks",
			resourceType: "admissionregistration.k8s.io/v1/MutatingWebhookConfiguration",
			path:         "webhooks",
			wantKey:      "name",
			wantFound:    true,
		},
		{
			name:         "MutatingWebhookConfiguration webhooks matchConditions",
			resourceType: "admissionregistration.k8s.io/v1/MutatingWebhookConfiguration",
			path:         "webhooks.0.matchConditions",
			wantKey:      "name",
			wantFound:    true,
		},

		// Not found cases
		{
			name:         "Non-associative path",
			resourceType: "apps/v1/Deployment",
			path:         "spec.replicas",
			wantKey:      "",
			wantFound:    false,
		},
		{
			name:         "Non-associative nested path",
			resourceType: "apps/v1/Deployment",
			path:         "spec.template.spec.containers.0.image",
			wantKey:      "",
			wantFound:    false,
		},
		{
			name:         "Unknown resource type - no pod spec",
			resourceType: "v1/ConfigMap",
			path:         "spec.containers",
			wantKey:      "",
			wantFound:    false,
		},
		{
			name:         "Unknown resource type - still gets universal",
			resourceType: "v1/ConfigMap",
			path:         "metadata.ownerReferences",
			wantKey:      "uid",
			wantFound:    true,
		},

		// Paths with associative segments (already resolved or mixed)
		{
			name:         "Path with existing associative segment",
			resourceType: "apps/v1/Deployment",
			path:         "spec.template.spec.containers.?name=nginx.env",
			wantKey:      "name",
			wantFound:    true,
		},

		// Rollout (custom resource with pod spec)
		{
			name:         "Rollout containers",
			resourceType: "argoproj.io/v1alpha1/Rollout",
			path:         "spec.template.spec.containers",
			wantKey:      "name",
			wantFound:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			keys, found := rp.MergeKeysForPath(tt.resourceType, tt.path)
			assert.Equal(t, tt.wantFound, found, "found mismatch")
			key := ""
			if len(keys) > 0 {
				key = keys[0]
			}
			assert.Equal(t, tt.wantKey, key, "key mismatch")
		})
	}
}

func TestCompiledStructureCoversTheWorkloadPrefixes(t *testing.T) {
	rp := NewK8sResourceProvider()

	// The universal spec, which every resource type falls back to.
	keys, found := rp.MergeKeysForPath(api.ResourceType("v1/ConfigMap"), "metadata.ownerReferences")
	assert.True(t, found, "universal entries should apply to any type")
	assert.Equal(t, []string{"uid"}, keys)

	// A Deployment reaches its PodSpec through a pod template at spec.template.
	keys, _ = rp.MergeKeysForPath(api.ResourceType("apps/v1/Deployment"), "spec.template.spec.containers")
	assert.Equal(t, []string{"name"}, keys)
	keys, _ = rp.MergeKeysForPath(api.ResourceType("apps/v1/Deployment"), "spec.template.spec.containers.*.env")
	assert.Equal(t, []string{"name"}, keys)
	keys, _ = rp.MergeKeysForPath(api.ResourceType("apps/v1/Deployment"), "spec.template.spec.containers.*.ports")
	assert.Equal(t, []string{"containerPort", "protocol"}, keys,
		"a container port is identified by its number and its protocol")
	keys, _ = rp.MergeKeysForPath(api.ResourceType("apps/v1/Deployment"), "spec.template.spec.volumes")
	assert.Equal(t, []string{"name"}, keys)

	// A Pod carries a PodSpec directly, so the same container shape lands one level up.
	keys, _ = rp.MergeKeysForPath(api.ResourceType("v1/Pod"), "spec.containers")
	assert.Equal(t, []string{"name"}, keys)
	keys, _ = rp.MergeKeysForPath(api.ResourceType("v1/Pod"), "spec.containers.*.env")
	assert.Equal(t, []string{"name"}, keys)

	// A CronJob nests a job template in front of the pod template.
	keys, _ = rp.MergeKeysForPath(api.ResourceType("batch/v1/CronJob"), "spec.jobTemplate.spec.template.spec.containers")
	assert.Equal(t, []string{"name"}, keys)

	// A type with no pod template still gets the keys it declares itself.
	keys, _ = rp.MergeKeysForPath(api.ResourceType("v1/Service"), "spec.ports")
	assert.Equal(t, []string{"port", "protocol"}, keys)
}

// A Pod has no pod template, so it must not get the pod-template metadata paths. The tables
// this replaced derived that path from the PodSpec path by trimming a `.spec` suffix, which
// a Pod's `spec` does not have, and registered spec.metadata.labels.* -- a path that does
// not exist in a Pod.
func TestPodHasNoPodTemplateMetadataPaths(t *testing.T) {
	rp := NewK8sResourceProvider()

	assert.False(t, rp.IsMapKeyPath(api.ResourceType("v1/Pod"), "spec.metadata.labels.*"))
	assert.False(t, rp.IsMapKeyPath(api.ResourceType("v1/Pod"), "spec.metadata.annotations.*"))

	// A Pod's own labels are universal, and still reachable.
	assert.True(t, rp.IsMapKeyPath(api.ResourceType("v1/Pod"), "metadata.labels.*"))

	// A Deployment's pod template does have them.
	assert.True(t, rp.IsMapKeyPath(api.ResourceType("apps/v1/Deployment"), "spec.template.metadata.labels.*"))
	assert.True(t, rp.IsMapKeyPath(api.ResourceType("batch/v1/CronJob"), "spec.jobTemplate.spec.template.metadata.labels.*"))
}

// #5085: a PodSpec inside a custom resource got no merge keys, so every path into its
// containers was recorded positionally, and a protected path drifted the moment a container or
// an env var was inserted ahead of it. Declaring where the pod template is gives the CRD the
// same keys a Deployment has.
func TestCRDWorkloadsGetTheirPodSpecMergeKeys(t *testing.T) {
	rp := NewK8sResourceProvider()

	runnerSet := api.ResourceType("actions.github.com/v1alpha1/AutoscalingRunnerSet")
	keys, found := rp.MergeKeysForPath(runnerSet, "spec.template.spec.containers")
	assert.True(t, found)
	assert.Equal(t, []string{"name"}, keys)
	keys, _ = rp.MergeKeysForPath(runnerSet, "spec.template.spec.initContainers.*.env")
	assert.Equal(t, []string{"name"}, keys)
	keys, _ = rp.MergeKeysForPath(runnerSet, "spec.template.spec.volumes")
	assert.Equal(t, []string{"name"}, keys)

	// A Knative Service reaches its PodSpec through a RevisionTemplateSpec, which is a pod
	// template in the same place.
	keys, _ = rp.MergeKeysForPath(api.ResourceType("serving.knative.dev/v1/Service"), "spec.template.spec.containers")
	assert.Equal(t, []string{"name"}, keys)
}

// A runner scale set carries two pod templates -- the runners it scales and the listener that
// watches GitHub for jobs -- so embedding the shape twice is what gives both of them merge keys.
// The second was missed when the stanza was written by hand and found by reading the CRD.
func TestARunnerScaleSetHasTwoPodTemplates(t *testing.T) {
	runnerSet := api.ResourceType("actions.github.com/v1alpha1/AutoscalingRunnerSet")

	// Declaration order, which is the spec file's order and is therefore stable.
	assert.Equal(t, []string{"spec.template.spec", "spec.listenerTemplate.spec"}, PodSpecPaths(runnerSet))

	rp := NewK8sResourceProvider()
	keys, found := rp.MergeKeysForPath(runnerSet, "spec.listenerTemplate.spec.containers")
	assert.True(t, found)
	assert.Equal(t, []string{"name"}, keys)

	// The metadata maps the CRD declares are maps, so a dotted label key is a key and not a
	// path of its own.
	assert.True(t, rp.IsMapKeyPath(runnerSet, "spec.listenerRoleMetadata.labels.*"))
}
