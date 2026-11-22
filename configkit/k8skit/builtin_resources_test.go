// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package k8skit

import "testing"

func TestIsBuiltInResource(t *testing.T) {
	for _, tc := range []struct {
		resourceType, namespace, name string
		want                          bool
	}{
		{"v1/Namespace", "", "kube-system", true},
		{"v1/Namespace", "", "argocd", false},
		{"v1/ServiceAccount", "prod", "default", true},
		{"v1/ServiceAccount", "prod", "web", false},
		{"v1/ConfigMap", "prod", "kube-root-ca.crt", true},
		{"v1/ConfigMap", "prod", "web-config", false},
		{"rbac.authorization.k8s.io/v1/ClusterRole", "", "view", true},
		{"rbac.authorization.k8s.io/v1/ClusterRole", "", "system:auth-delegator", true},
		{"rbac.authorization.k8s.io/v1/ClusterRole", "", "web-reader", false},
		{"rbac.authorization.k8s.io/v1/Role", "kube-system", "extension-apiserver-authentication-reader", true},
		{"rbac.authorization.k8s.io/v1/Role", "prod", "extension-apiserver-authentication-reader", false},
		{"rbac.authorization.k8s.io/v1/Role", "prod", "view", false},
		{"scheduling.k8s.io/v1/PriorityClass", "", "system-node-critical", true},
		{"scheduling.k8s.io/v1/PriorityClass", "", "high", false},
		// The group is compared, not only the kind.
		{"example.com/v1/ConfigMap", "prod", "kube-root-ca.crt", false},
		{"example.com/v1/ClusterRole", "", "view", false},
	} {
		if got := IsBuiltInResource(tc.resourceType, tc.namespace, tc.name); got != tc.want {
			t.Errorf("IsBuiltInResource(%q, %q, %q) = %v, want %v", tc.resourceType, tc.namespace, tc.name, got, tc.want)
		}
	}
}
