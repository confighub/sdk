// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"testing"
)

func TestParseProtectionSpec(t *testing.T) {
	for _, tc := range []struct {
		spec, resourceType, resourceName, path string
	}{
		{"apps/v1/Deployment:default/myapp:spec.replicas", "apps/v1/Deployment", "default/myapp", "spec.replicas"},
		{"apps/v1/Deployment:default/myapp:spec.template.spec.containers.?name=app;@0.image",
			"apps/v1/Deployment", "default/myapp", "spec.template.spec.containers.?name=app;@0.image"},
		// No path, or an empty one, is the resource as a whole.
		{"v1/ServiceAccount:default/myapp", "v1/ServiceAccount", "default/myapp", ""},
		{"v1/ServiceAccount:default/myapp:", "v1/ServiceAccount", "default/myapp", ""},
	} {
		resourceType, resourceName, path, err := parseProtectionSpec("protect", tc.spec)
		if err != nil {
			t.Fatalf("%q: %v", tc.spec, err)
		}
		if resourceType != tc.resourceType || resourceName != tc.resourceName || path != tc.path {
			t.Errorf("%q: got %q %q %q, want %q %q %q", tc.spec, resourceType, resourceName, path,
				tc.resourceType, tc.resourceName, tc.path)
		}
	}
	for _, spec := range []string{"", "v1/ServiceAccount", ":default/myapp", "v1/ServiceAccount:"} {
		if _, _, _, err := parseProtectionSpec("protect", spec); err == nil {
			t.Errorf("%q: expected an error", spec)
		}
	}
}
