// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package k8skit

import (
	"testing"

	"github.com/confighub/sdk/core/function/api"
)

func TestIsCrossplaneClusterScopedGroup(t *testing.T) {
	tests := []struct {
		group string
		want  bool
	}{
		{"eks.aws.upbound.io", true},
		{"ec2.aws.upbound.io", true},
		{"aws.upbound.io", true},
		{"gcp.upbound.io", true},
		{"pkg.crossplane.io", true},
		{"apiextensions.crossplane.io", true},
		// The Crossplane v2 namespaced variants ARE namespaced.
		{"eks.aws.m.upbound.io", false},
		{"ec2.aws.m.upbound.io", false},
		{"pkg.m.crossplane.io", false},
		// Not Crossplane.
		{"apps", false},
		{"", false},
		{"eks.services.k8s.aws", false},
		// A user-defined composite resource is not classified by the rule.
		{"platform.acme.io", false},
	}
	for _, tt := range tests {
		if got := IsCrossplaneClusterScopedGroup(tt.group); got != tt.want {
			t.Errorf("IsCrossplaneClusterScopedGroup(%q) = %v, want %v", tt.group, got, tt.want)
		}
	}
}

func TestIsResourceTypeClusterScoped(t *testing.T) {
	tests := []struct {
		rt   string
		want bool
	}{
		// From the curated map.
		{"v1/Namespace", true},
		{"rbac.authorization.k8s.io/v1/ClusterRole", true},
		{"apiextensions.crossplane.io/v1/Composition", true},
		// From the group-suffix rule, which the map does not enumerate.
		{"eks.aws.upbound.io/v1beta2/Cluster", true},
		{"eks.aws.upbound.io/v1beta1/NodeGroup", true},
		{"ec2.aws.upbound.io/v1beta1/Subnet", true},
		{"iam.aws.upbound.io/v1beta1/Role", true},
		// Namespaced variants stay namespaced.
		{"eks.aws.m.upbound.io/v1beta1/Cluster", false},
		// Ordinary namespaced resources.
		{"apps/v1/Deployment", false},
		{"v1/ConfigMap", false},
		{"v1/Pod", false},
	}
	for _, tt := range tests {
		if got := IsResourceTypeClusterScoped(api.ResourceType(tt.rt)); got != tt.want {
			t.Errorf("IsResourceTypeClusterScoped(%q) = %v, want %v", tt.rt, got, tt.want)
		}
	}
}

func TestApiGroupOf(t *testing.T) {
	tests := map[string]string{
		"eks.aws.upbound.io/v1beta2/Cluster": "eks.aws.upbound.io",
		"apps/v1/Deployment":                 "apps",
		// A core type has no group.
		"v1/Pod": "",
		"":       "",
	}
	for in, want := range tests {
		if got := apiGroupOf(api.ResourceType(in)); got != want {
			t.Errorf("apiGroupOf(%q) = %q, want %q", in, got, want)
		}
	}
}
