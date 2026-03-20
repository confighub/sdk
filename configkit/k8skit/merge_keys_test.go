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
			key, found := rp.MergeKeyForPath(tt.resourceType, tt.path)
			assert.Equal(t, tt.wantFound, found, "found mismatch")
			assert.Equal(t, tt.wantKey, key, "key mismatch")
		})
	}
}

func TestNormalizeMergeKeyPath(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"spec.containers", "spec.containers"},
		{"spec.containers.0.env", "spec.containers.*.env"},
		{"spec.containers.12.env.3", "spec.containers.*.env.*"},
		{"metadata.ownerReferences", "metadata.ownerReferences"},
		{"webhooks.0.matchConditions", "webhooks.*.matchConditions"},
		{"spec.template.spec.containers.?name=nginx.env", "spec.template.spec.containers.*.env"},
		{"spec.template.spec.containers.?name=nginx;@0.env", "spec.template.spec.containers.*.env"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := normalizeMergeKeyPath(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestBuildMergeKeyLookup(t *testing.T) {
	lookup := buildMergeKeyLookup()

	// Verify universal entries exist
	universalMap, ok := lookup[api.ResourceType("*")]
	assert.True(t, ok, "universal entries should exist")
	assert.Equal(t, "uid", universalMap["metadata.ownerReferences"])

	// Verify Deployment has pod spec entries
	deployMap, ok := lookup[api.ResourceType("apps/v1/Deployment")]
	assert.True(t, ok, "Deployment entries should exist")
	assert.Equal(t, "name", deployMap["spec.template.spec.containers"])
	assert.Equal(t, "name", deployMap["spec.template.spec.containers.*.env"])
	assert.Equal(t, "containerPort", deployMap["spec.template.spec.containers.*.ports"])
	assert.Equal(t, "name", deployMap["spec.template.spec.volumes"])

	// Verify Pod has different prefix
	podMap, ok := lookup[api.ResourceType("v1/Pod")]
	assert.True(t, ok, "Pod entries should exist")
	assert.Equal(t, "name", podMap["spec.containers"])
	assert.Equal(t, "name", podMap["spec.containers.*.env"])

	// Verify CronJob has deep prefix
	cronMap, ok := lookup[api.ResourceType("batch/v1/CronJob")]
	assert.True(t, ok, "CronJob entries should exist")
	assert.Equal(t, "name", cronMap["spec.jobTemplate.spec.template.spec.containers"])

	// Verify Service has its own entries
	svcMap, ok := lookup[api.ResourceType("v1/Service")]
	assert.True(t, ok, "Service entries should exist")
	assert.Equal(t, "port", svcMap["spec.ports"])
}
