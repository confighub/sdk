// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package blame

import (
	"strings"
	"testing"

	"github.com/confighub/sdk/core/configkit/yamlkit"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
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
			if got := AuthoredPath(tt.path); got != tt.want {
				t.Errorf("AuthoredPath(%q) = %q, want %q", tt.path, got, tt.want)
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

// TestCreditPathUsesClosestAncestor: an env var added after the Unit was created is
// recorded as one Add of the element, so its leaves have no entries of their own. They
// are credited to that Add, not to the resource-level entry of the create.
func TestCreditPathUsesClosestAncestor(t *testing.T) {
	add, del := goclientnew.Add, goclientnew.Delete
	pathMutations := goclientnew.MutationMap{
		"spec.template.spec.containers.?name=app.env.?name=Z;@0": {MutationType: &add, Index: 4},
		// An earlier removal of the whole container is not what wrote anything in it now.
		"spec.template.spec.containers.?name=app": {MutationType: &del, Index: 2},
	}
	sources := goclientnew.ResourceMutationList{{
		Resource:             &goclientnew.ResourceInfo{ResourceType: "apps/v1/Deployment", ResourceName: "default/web"},
		ResourceMutationInfo: &goclientnew.MutationInfo{MutationType: &add, Index: 1},
		PathMutationMap:      &pathMutations,
	}}

	info, _ := creditPath(nil, sources, "apps/v1/Deployment", "default/web", "web",
		"spec.template.spec.containers.?name=app.env.?name=Z.value")
	if info == nil || info.Index != 4 {
		t.Fatalf("a leaf of an added element should be credited to the element's Add (4), got %+v", info)
	}

	info, _ = creditPath(nil, sources, "apps/v1/Deployment", "default/web", "web",
		"spec.template.spec.containers.?name=app.image")
	if info == nil || info.Index != 1 {
		t.Fatalf("a path whose only ancestor entry is a Delete should fall back to the resource (1), got %+v", info)
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
	rm := matchBlameResource(nil, sources, "apps/v1/Deployment", "apptique/frontend", "frontend")
	if rm == nil {
		t.Fatal("a differently scoped name should still match on the name itself")
	}

	if other := matchBlameResource(nil, sources, "apps/v1/Deployment", "apptique/backend", "backend"); other != nil {
		t.Error("a different resource should not match")
	}
	if other := matchBlameResource(nil, sources, "v1/Service", "apptique/frontend", "frontend"); other != nil {
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
			if AuthoredPath(l.Path) == "spec.template.spec.containers.?name=server.image" {
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
	fields := []*Field{
		{ResourceType: "apps/v1/Deployment", ResourceName: "ns/frontend", Path: "spec.replicas"},
		{ResourceType: "apps/v1/Deployment", ResourceName: "ns/frontend", Path: "spec.template.spec.containers.?name=server.image"},
		{ResourceType: "v1/Service", ResourceName: "ns/frontend", Path: "spec.ports.0.port"},
		{ResourceType: "apps/v1/Deployment", ResourceName: "ns/frontend", Path: "$comment$head$apiVersion"},
		{ResourceType: "apps/v1/Deployment", ResourceName: "ns/frontend", Path: "spec.template.spec.$comment$head$imagePullSecrets"},
	}

	t.Run("comment pseudo-fields are hidden by default, at any depth", func(t *testing.T) {
		got := Filter(fields, "", "", false)
		for _, f := range got {
			if strings.Contains(f.Path, "$comment$") {
				t.Errorf("comment field %s should not be reported by default", f.Path)
			}
		}
		if len(got) != 3 {
			t.Errorf("kept %d fields, want 3", len(got))
		}
	})

	t.Run("--show-comments keeps them", func(t *testing.T) {
		if got := Filter(fields, "", "", true); len(got) != 5 {
			t.Errorf("kept %d fields, want 5", len(got))
		}
	})

	t.Run("--path matches a substring", func(t *testing.T) {
		got := Filter(fields, "image", "", false)
		if len(got) != 1 || got[0].Path != "spec.template.spec.containers.?name=server.image" {
			t.Errorf("got %+v, want just the image field", got)
		}
	})

	t.Run("--resource matches type or name", func(t *testing.T) {
		if got := Filter(fields, "", "v1/Service", false); len(got) != 1 {
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
		got[AuthoredPath(leaf.Path)] = leaf.Value
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

// TestBlameSetByFallsBackToSource: an update that wrote the data it was given names no
// function, merge, Link or Trigger, and was shown with nothing at all. The Revision says
// which operation made it.
func TestBlameSetByFallsBackToSource(t *testing.T) {
	if got := blameSetBy(&Origin{Description: "UpdateUnit", Source: "UpdateUnit"}, ""); got != "UpdateUnit" {
		t.Errorf("a plain update should be named by its Revision's Source, got %q", got)
	}
	if got := blameSetBy(&Origin{Description: "MergeExternal; from stdin", Source: "MergeExternal"}, ""); got != "stdin" {
		t.Errorf("an external merge should still be named by its source, got %q", got)
	}
	if got := blameSetBy(&Origin{Function: "set-replicas", Source: "Invoke"}, ""); got != "set-replicas" {
		t.Errorf("a function should be named over the Source, got %q", got)
	}
	if got := blameSetBy(&Origin{Source: "CloneUnit"}, "base/web"); got != "merge from base/web" {
		t.Errorf("a merge should be named by the unit it took from, got %q", got)
	}
}

// TestBlameSetByNamesTheLinkOverItsFunction: a Link or Trigger chose the function it ran, so
// it is what made the change, and the function is how. A TransformPaths Link that set an
// image was reported as set-image-reference-by-uri, which named no Link to look at.
func TestBlameSetByNamesTheLinkOverItsFunction(t *testing.T) {
	const fn = "set-image-reference-by-uri"
	if got := blameSetBy(&Origin{Function: fn, Link: "ui-stable-image", Source: "TransformPaths"}, ""); got != "link ui-stable-image" {
		t.Errorf("a Link's change should be named by the Link, got %q", got)
	}
	if got := blameSetBy(&Origin{Function: fn, Trigger: "platform/pin-images", Source: "Invoke"}, ""); got != "trigger platform/pin-images" {
		t.Errorf("a Trigger's change should be named by the Trigger, got %q", got)
	}
}

// TestBlameLinkTarget: a Link's resolve is followed upstream to the field it read, and only
// where the record says which field that was.
func TestBlameLinkTarget(t *testing.T) {
	resource := &goclientnew.ResourceInfo{ResourceType: "registrybot.confighub.com/v1alpha3", ResourceName: "confighub-ui"}
	transform := func(paths []goclientnew.NamedPath, getters []goclientnew.NamedFunctionResult) *goclientnew.ExtendedMutation {
		return &goclientnew.ExtendedMutation{
			Mutation: &goclientnew.Mutation{MergeEndRevisionNum: 79},
			Link:     &goclientnew.Link{UpdateType: "TransformPaths", UpstreamPaths: paths, UpstreamGetters: getters},
		}
	}

	one := transform([]goclientnew.NamedPath{{Name: "tag", Path: "streams.stable.tag", Resource: resource}}, nil)
	if got, follow := blameLinkTarget(one); !follow || got == nil || got.path != "streams.stable.tag" || got.resourceName != "confighub-ui" {
		t.Errorf("a TransformPaths Link with one upstream path should be followed to it, got %+v", got)
	}
	two := transform([]goclientnew.NamedPath{
		{Name: "repo", Path: "spec.repo", Resource: resource}, {Name: "tag", Path: "streams.stable.tag", Resource: resource},
	}, nil)
	if got, follow := blameLinkTarget(two); follow {
		t.Errorf("with two upstream paths the value came from both, and the walk should stop, got %+v", got)
	}
	getter := transform([]goclientnew.NamedPath{{Name: "tag", Path: "streams.stable.tag", Resource: resource}},
		[]goclientnew.NamedFunctionResult{{Name: "hash"}})
	if got, follow := blameLinkTarget(getter); follow {
		t.Errorf("an upstream getter computes from more than one field, and the walk should stop, got %+v", got)
	}

	provided := &goclientnew.ExtendedMutation{
		Mutation: &goclientnew.Mutation{
			MergeEndRevisionNum: 3,
			ProvidedPath:        "metadata.name",
			ProvidedResource:    &goclientnew.ResourceInfoType2{ResourceType: "v1/Namespace", ResourceName: "/prod"},
		},
		Link: &goclientnew.Link{UpdateType: "NeedsProvides"},
	}
	if got, follow := blameLinkTarget(provided); !follow || got == nil || got.path != "metadata.name" || got.resourceType != "v1/Namespace" {
		t.Errorf("a needs/provides resolve should be followed to the path it recorded, got %+v", got)
	}

	insert := &goclientnew.ExtendedMutation{
		Mutation: &goclientnew.Mutation{MergeEndRevisionNum: 2},
		Link:     &goclientnew.Link{UpdateType: "Insert"},
	}
	if got, follow := blameLinkTarget(insert); follow {
		t.Errorf("an Insert reads a whole document and has no field to follow, got %+v", got)
	}

	invocation := uuid.New()
	upsert := &goclientnew.ExtendedMutation{
		Mutation: &goclientnew.Mutation{MergeEndRevisionNum: 2},
		Link:     &goclientnew.Link{UpdateType: "Upsert"},
	}
	// An Upsert written before Upserts recorded a merge source.
	if got, follow := blameLinkTarget(upsert); !follow || got != nil {
		t.Errorf("an Upsert that copied resources should be followed to the same field, got %+v, %v", got, follow)
	}
	upsert.Link.TransformInvocationID = &invocation
	if got, follow := blameLinkTarget(upsert); follow {
		t.Errorf("an Upsert that ran a transform has no field to follow, got %+v", got)
	}
}

// TestBlameResolveDescription: a resolve written before Mutations recorded the upstream
// Revision named it only in its change description.
func TestBlameResolveDescription(t *testing.T) {
	for description, want := range map[string][]string{
		"Transformed from revision 73 of unit registry-facts/confighubai-confighub-ui": {"73", "registry-facts", "confighubai-confighub-ui"},
		"Inserted from revision 2 of unit infra/policy":                                {"2", "infra", "policy"},
		"Upserted from revision 9 of unit base/web":                                    {"9", "base", "web"},
		"Updated to revision 4 of unit base/web":                                       nil,
		"Bump the image":                                                               nil,
	} {
		match := blameResolveDescription.FindStringSubmatch(description)
		if want == nil {
			if match != nil {
				t.Errorf("%q should not match, got %v", description, match)
			}
			continue
		}
		if match == nil || match[1] != want[0] || match[2] != want[1] || match[3] != want[2] {
			t.Errorf("%q: got %v, want %v", description, match, want)
		}
	}
}

func TestBlameTransformedByLink(t *testing.T) {
	invocation := uuid.New()
	for name, tc := range map[string]struct {
		link *goclientnew.Link
		want bool
	}{
		"no link":                 {nil, false},
		"upsert as is":            {&goclientnew.Link{UpdateType: "Upsert"}, false},
		"upsert with a transform": {&goclientnew.Link{UpdateType: "Upsert", TransformInvocationID: &invocation}, true},
		"nil transform id":        {&goclientnew.Link{UpdateType: "Upsert", TransformInvocationID: &uuid.Nil}, false},
	} {
		em := &goclientnew.ExtendedMutation{Mutation: &goclientnew.Mutation{}, Link: tc.link}
		if got := blameTransformedByLink(em); got != tc.want {
			t.Errorf("%s: got %v, want %v", name, got, tc.want)
		}
	}
}
