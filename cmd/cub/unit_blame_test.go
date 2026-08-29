// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"strings"
	"testing"

	"github.com/confighub/sdk/core/configkit/yamlkit"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

func TestAuthoredPath(t *testing.T) {
	tests := []struct {
		name string
		path string
		want string
	}{
		{
			name: "a plain path is unchanged",
			path: "spec.replicas",
			want: "spec.replicas",
		},
		{
			name: "an associative segment loses its anchor",
			path: "spec.template.spec.containers.?name=server;@0.image",
			want: "spec.template.spec.containers.?name=server.image",
		},
		{
			name: "every associative segment loses its anchor",
			path: "spec.template.spec.containers.?name=server;@0.env.?name=PORT;@3.value",
			want: "spec.template.spec.containers.?name=server.env.?name=PORT.value",
		},
		{
			name: "a numeric index is not an anchor and stays",
			path: "spec.template.spec.containers.?name=server;@0.ports.0.containerPort",
			want: "spec.template.spec.containers.?name=server.ports.0.containerPort",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := authoredPath(tt.path); got != tt.want {
				t.Errorf("authoredPath(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestExternalSourceFromDescription(t *testing.T) {
	tests := []struct {
		name        string
		description string
		want        string
	}{
		{
			name:        "an external merge names its source",
			description: "MergeExternal; from helm template apptique ./apptique/helm-chart",
			want:        "helm template apptique ./apptique/helm-chart",
		},
		{
			name:        "the caller's own text after the separator is dropped",
			description: "MergeExternal; from oci://ghcr.io/org/bundle| Functions: set-namespace",
			want:        "oci://ghcr.io/org/bundle",
		},
		{
			name:        "an ordinary change names no source",
			description: "Pin frontend to v0.10.4| Functions: set-container-image",
			want:        "",
		},
		{
			name:        "a clone names no source",
			description: "Cloned from 2f120fa8-9cae-4ede-804d-6593ac8b60d4/deployment-frontend",
			want:        "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := externalSourceFromDescription(tt.description); got != tt.want {
				t.Errorf("externalSourceFromDescription(%q) = %q, want %q",
					tt.description, got, tt.want)
			}
		})
	}
}

// TestLookupPathMutationIgnoresAnchors is the case that made every value in a variant
// read as "arrived with the clone": MutationSources had the path without its anchor
// and the flattening produced it with one.
func TestLookupPathMutationIgnoresAnchors(t *testing.T) {
	update := goclientnew.Update
	stored := goclientnew.MutationMap{
		"spec.template.spec.containers.?name=server.image": {
			MutationType: &update,
			Index:        3,
			Value:        "frontend:v0.10.4",
		},
	}
	info := lookupPathMutation(&stored, "spec.template.spec.containers.?name=server;@0.image")
	if info == nil {
		t.Fatal("an anchored path should find the entry stored without an anchor")
	}
	if info.Index != 3 {
		t.Errorf("Index = %d, want 3", info.Index)
	}
}

// TestLookupPathMutationSkipsNone: a None mutation records that nothing wrote the
// path, so crediting it would attribute the value to a change that did not happen.
func TestLookupPathMutationSkipsNone(t *testing.T) {
	none := goclientnew.None
	stored := goclientnew.MutationMap{
		"spec.replicas": {MutationType: &none, Index: 1},
	}
	if info := lookupPathMutation(&stored, "spec.replicas"); info != nil {
		t.Errorf("a None mutation should not be credited, got index %d", info.Index)
	}
}

// TestMatchBlameResourceFallsBackToUnscopedName is what lets the upstream walk cross
// a namespace fill-in: the base still says "confighubplaceholder/frontend" where the
// variant says "apptique/frontend".
func TestMatchBlameResourceFallsBackToUnscopedName(t *testing.T) {
	add := goclientnew.Add
	sources := goclientnew.ResourceMutationList{
		{
			Resource: &goclientnew.ResourceInfo{
				ResourceType:             "apps/v1/Deployment",
				ResourceName:             "confighubplaceholder/frontend",
				ResourceNameWithoutScope: "frontend",
			},
			ResourceMutationInfo: &goclientnew.MutationInfo{MutationType: &add, Index: 1},
		},
	}
	rm := matchBlameResource(sources, "apps/v1/Deployment", "apptique/frontend", "frontend")
	if rm == nil {
		t.Fatal("a differently scoped name should still match on the name itself")
	}

	if other := matchBlameResource(sources, "apps/v1/Deployment", "apptique/backend", "backend"); other != nil {
		t.Error("a different resource should not match")
	}
	if other := matchBlameResource(sources, "v1/Service", "apptique/frontend", "frontend"); other != nil {
		t.Error("a different resource type should not match")
	}
}

func TestExpandBlameValue(t *testing.T) {
	k8s := mustBlameProvider(t, "Kubernetes/YAML")

	t.Run("a scalar is one field", func(t *testing.T) {
		leaves := expandBlameValue(k8s, "apps/v1/Deployment", "spec.replicas", "3\n")
		if len(leaves) != 1 || leaves[0].Path != "spec.replicas" || leaves[0].Value != "3" {
			t.Fatalf("got %+v, want one spec.replicas=3", leaves)
		}
	})

	t.Run("a mapping becomes one field per leaf", func(t *testing.T) {
		leaves := expandBlameValue(k8s, "apps/v1/Deployment", "spec.template.spec.securityContext",
			"runAsUser: 1000\nrunAsNonRoot: true\n")
		got := map[string]string{}
		for _, l := range leaves {
			got[l.Path] = l.Value
		}
		want := map[string]string{
			"spec.template.spec.securityContext.runAsUser":    "1000",
			"spec.template.spec.securityContext.runAsNonRoot": "true",
		}
		for path, value := range want {
			if got[path] != value {
				t.Errorf("%s = %q, want %q (got %+v)", path, got[path], value, got)
			}
		}
	})

	t.Run("a merge-keyed array is addressed by its key", func(t *testing.T) {
		leaves := expandBlameValue(k8s, "apps/v1/Deployment", "spec.template.spec.containers",
			"- name: server\n  image: frontend:v1\n")
		found := false
		for _, l := range leaves {
			if authoredPath(l.Path) == "spec.template.spec.containers.?name=server.image" {
				found = true
				if l.Value != "frontend:v1" {
					t.Errorf("image = %q, want frontend:v1", l.Value)
				}
			}
		}
		if !found {
			t.Errorf("no merge-keyed image path; got %+v", leaves)
		}
	})

	t.Run("an unparseable value stays one field", func(t *testing.T) {
		leaves := expandBlameValue(k8s, "apps/v1/Deployment", "data.blob", "\tnot: [valid")
		if len(leaves) != 1 || leaves[0].Path != "data.blob" {
			t.Fatalf("got %+v, want the value reported whole", leaves)
		}
	})
}

func TestFilterBlameFields(t *testing.T) {
	fields := []*blameField{
		{ResourceType: "apps/v1/Deployment", ResourceName: "ns/frontend", Path: "spec.replicas"},
		{ResourceType: "apps/v1/Deployment", ResourceName: "ns/frontend", Path: "spec.template.spec.containers.?name=server.image"},
		{ResourceType: "v1/Service", ResourceName: "ns/frontend", Path: "spec.ports.0.port"},
		{ResourceType: "apps/v1/Deployment", ResourceName: "ns/frontend", Path: "$comment$head$apiVersion"},
	}

	t.Run("comment pseudo-fields are hidden by default", func(t *testing.T) {
		got := filterBlameFields(fields, "", "", false)
		for _, f := range got {
			if f.Path == "$comment$head$apiVersion" {
				t.Error("a comment field should not be reported by default")
			}
		}
		if len(got) != 3 {
			t.Errorf("kept %d fields, want 3", len(got))
		}
	})

	t.Run("--show-comments keeps them", func(t *testing.T) {
		if got := filterBlameFields(fields, "", "", true); len(got) != 4 {
			t.Errorf("kept %d fields, want 4", len(got))
		}
	})

	t.Run("--path matches a substring", func(t *testing.T) {
		got := filterBlameFields(fields, "image", "", false)
		if len(got) != 1 || got[0].Path != "spec.template.spec.containers.?name=server.image" {
			t.Errorf("got %+v, want just the image field", got)
		}
	})

	t.Run("--resource matches type or name", func(t *testing.T) {
		if got := filterBlameFields(fields, "", "v1/Service", false); len(got) != 1 {
			t.Errorf("kept %d fields for the Service, want 1", len(got))
		}
	})
}

func mustBlameProvider(t *testing.T, toolchainType string) yamlkit.ResourceProvider {
	t.Helper()
	provider, err := blameResourceProvider(toolchainType)
	if err != nil {
		t.Fatalf("blameResourceProvider(%q): %v", toolchainType, err)
	}
	return provider
}

// TestBlameResourceProviderPerToolchain: a Unit is not necessarily Kubernetes, and
// blame reads its data with the executor registered for its own toolchain. A
// toolchain with no provider is an error rather than a silent fall back to
// Kubernetes, which would misparse the data.
func TestBlameResourceProviderPerToolchain(t *testing.T) {
	for _, toolchainType := range []string{
		"Kubernetes/YAML", "ConfigHub/YAML", "AppConfig/JSON", "AppConfig/YAML",
		"AppConfig/TOML", "AppConfig/INI", "AppConfig/Properties", "AppConfig/Env",
	} {
		if _, err := blameResourceProvider(toolchainType); err != nil {
			t.Errorf("no provider for %q: %v", toolchainType, err)
		}
	}
	if _, err := blameResourceProvider("Nonsense/Format"); err == nil {
		t.Error("an unknown toolchain should be an error, not a silent Kubernetes default")
	}
}

// TestExpandBlameValueNonKubernetes: merge keys are the toolchain's answer. An
// AppConfig format has none, so its arrays are addressed positionally -- which is
// what its own MutationSources paths use, and so what the credit lookup matches.
func TestExpandBlameValueNonKubernetes(t *testing.T) {
	appjson := mustBlameProvider(t, "AppConfig/JSON")
	// The value is YAML even though the Unit is JSON: MutationSources records values
	// as YAML whatever the Unit's format.
	leaves := expandBlameValue(appjson, "NoSchema", "",
		"Statement:\n- Sid: AllowCustomerPull\n  Effect: Allow\nVersion: \"2012-10-17\"\n")

	got := map[string]string{}
	for _, leaf := range leaves {
		got[leaf.Path] = leaf.Value
	}
	want := map[string]string{
		"Statement.0.Sid":    "AllowCustomerPull",
		"Statement.0.Effect": "Allow",
		"Version":            "2012-10-17",
	}
	for path, value := range want {
		if got[path] != value {
			t.Errorf("%s = %q, want %q (got %+v)", path, got[path], value, got)
		}
	}
	for _, leaf := range leaves {
		if strings.Contains(leaf.Path, "?") {
			t.Errorf("path %q uses a merge-key selector, but this toolchain has no merge keys", leaf.Path)
		}
	}
}

// TestOverlayBlameLeaves: a path entry is the newer word on a field than the
// whole-resource value it sits inside, and the two sides may disagree about whether
// an associative segment carries its anchor.
func TestOverlayBlameLeaves(t *testing.T) {
	base := []blameLeaf{
		{Path: "spec.replicas", Value: "1"},
		{Path: "spec.template.spec.containers.?name=server;@0.image", Value: "old"},
	}
	newer := []blameLeaf{
		{Path: "spec.template.spec.containers.?name=server.image", Value: "new"},
	}
	merged := overlayBlameLeaves(base, newer)

	got := map[string]string{}
	for _, leaf := range merged {
		got[authoredPath(leaf.Path)] = leaf.Value
	}
	if got["spec.template.spec.containers.?name=server.image"] != "new" {
		t.Errorf("the newer value should win, got %+v", got)
	}
	if got["spec.replicas"] != "1" {
		t.Errorf("an untouched field should survive, got %+v", got)
	}
	if len(merged) != 2 {
		t.Errorf("overlay produced %d leaves, want 2: %+v", len(merged), merged)
	}
}
