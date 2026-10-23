// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package k8skit

import "testing"

func TestIsBuiltInNamespace(t *testing.T) {
	for _, tc := range []struct {
		name string
		want bool
	}{
		{"default", true},
		{"kube-system", true},
		{"kube-public", true},
		{"kube-node-lease", true},

		// The kube- prefix is reserved, but other kube- namespaces are not
		// created by the cluster.
		{"kube-flannel", false},
		{"web", false},
		{"", false},
		{"Default", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsBuiltInNamespace(tc.name); got != tc.want {
				t.Errorf("IsBuiltInNamespace(%q) = %v, want %v", tc.name, got, tc.want)
			}
		})
	}
}

// The caller owns the returned slice, so changing it cannot change the answer.
func TestBuiltInNamespaces_ReturnsCopy(t *testing.T) {
	got := BuiltInNamespaces()
	if len(got) != 4 {
		t.Fatalf("BuiltInNamespaces() = %v, want 4 namespaces", got)
	}
	got[0] = "web"
	if IsBuiltInNamespace("web") || !IsBuiltInNamespace("default") {
		t.Fatal("modifying the returned slice changed the built-in set")
	}
}
