// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package blame

import (
	"strings"
	"testing"

	"github.com/confighub/sdk/core/function/api"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

func blameTestField(resourceType, resourceName, resourceNameCore, path string) *Field {
	return &Field{ResourceType: resourceType, ResourceName: resourceName, resourceNameCore: resourceNameCore, Path: path}
}

// TestBlameRefIsTheSameAcrossVariants: a REF copied from one variant's blame has to find the
// same field in another, where the resource has another namespace and possibly another API
// version.
func TestBlameRefIsTheSameAcrossVariants(t *testing.T) {
	identifier := mustBlameProvider(t, "Kubernetes/YAML")
	base := []*Field{blameTestField("apps/v1/Deployment", "confighubplaceholder/web", "web", "spec.replicas")}
	prod := []*Field{blameTestField("apps/v1beta1/Deployment", "prod/web", "web", "spec.replicas")}
	assignBlameRefs(identifier, base)
	assignBlameRefs(identifier, prod)
	if base[0].Ref != prod[0].Ref {
		t.Errorf("the same field has REF %s in the base and %s in prod", base[0].Ref, prod[0].Ref)
	}
	if len(base[0].Ref) != RefLength {
		t.Errorf("REF %q is not %d characters", base[0].Ref, RefLength)
	}

	other := []*Field{blameTestField("apps/v1/Deployment", "prod/web", "web", "spec.paused")}
	assignBlameRefs(identifier, other)
	if other[0].Ref == prod[0].Ref {
		t.Error("two paths of one resource have the same REF")
	}
}

// TestBlameRefSeparatesResourcesThatDifferOnlyInScope: two Deployments named web in two
// namespaces of one unit would share every REF if the scope were left out.
func TestBlameRefSeparatesResourcesThatDifferOnlyInScope(t *testing.T) {
	identifier := mustBlameProvider(t, "Kubernetes/YAML")
	fields := []*Field{
		blameTestField("apps/v1/Deployment", "a/web", "web", "spec.replicas"),
		blameTestField("apps/v1/Deployment", "b/web", "web", "spec.replicas"),
		blameTestField("v1/Service", "a/web", "web", "spec.replicas"),
	}
	assignBlameRefs(identifier, fields)
	if fields[0].Ref == fields[1].Ref {
		t.Errorf("Deployments a/web and b/web share REF %s", fields[0].Ref)
	}
	if fields[2].Ref == fields[0].Ref || fields[2].Ref == fields[1].Ref {
		t.Error("a Service shares a REF with a Deployment of the same name")
	}
}

func TestIsBlameRef(t *testing.T) {
	for argument, want := range map[string]bool{
		"070f84b": true, "070f": true, "070": false, "070f84b1": false,
		"spec.replicas": false, "070F84B": false, "zzzz": false,
	} {
		if got := IsRef(argument); got != want {
			t.Errorf("IsRef(%q) = %v, want %v", argument, got, want)
		}
	}
}

// TestBlameFlags: the field's own facts, then the facts about the change in this unit that
// brought its value -- not the upstream change SET BY names.
func TestBlameFlags(t *testing.T) {
	f := &Field{
		Protected: true,
		Guards:    map[string]string{"owner": "platform"},
		Conflicts: []goclientnew.MutationConflict{{Reason: "ProtectedPath"}},
		Chain: []*Origin{
			{RevisionNum: 29, Link: "ui-stable-image", linkUpdateType: "TransformPaths", ChangeOrders: []string{"base/release-1"}},
			{RevisionNum: 3, Trigger: "upstream-trigger"},
		},
	}
	if got := blameFlags(f, 28, true); got != "PGXLCN" {
		t.Errorf("flags = %q, want PGXLCN", got)
	}
	if got := blameFlags(f, 29, true); strings.Contains(got, "N") {
		t.Errorf("a value written at the last release is released, got %q", got)
	}
	if got := blameFlags(f, 0, false); strings.Contains(got, "N") {
		t.Errorf("a space with no release target has nothing unreleased, got %q", got)
	}
	if strings.Contains(blameFlags(f, 29, true), "T") {
		t.Error("a Trigger upstream did not set the value in this unit")
	}

	upgraded := &Field{Chain: []*Origin{{RevisionNum: 5, Link: "upgrade-ui", linkUpdateType: "UpgradeUnit"}}}
	if got := blameFlags(upgraded, 5, true); got != "U" {
		t.Errorf("an UpgradeUnit Link is U and not L, got %q", got)
	}

	original := &Field{}
	if got := blameFlags(original, 0, true); got != "N" {
		t.Errorf("a value of a never-released unit is not released, got %q", got)
	}
	if got := blameFlags(original, 4, true); got != "" {
		t.Errorf("a value there since creation is in any release, got %q", got)
	}
}

func TestBlameFlagsUsedListsOnlyWhatAppears(t *testing.T) {
	legend := FlagsUsed([]*Field{{Flags: "LNp"}, {Flags: "P"}, {Flags: "U"}})
	var letters []string
	for _, flag := range legend {
		letters = append(letters, flag.Letter)
	}
	if got := strings.Join(letters, ""); got != "PULNp" {
		t.Errorf("legend letters = %q, want PULNp in column order", got)
	}
}

// TestBlameConflictsAndFindingsForPath: a record about the field, an ancestor whose whole
// value it was, or the resource is about the field. One about a sibling is not, and a
// ProvidedNotFound finding is about the upstream unit's resource, whatever its path says.
func TestBlameConflictsAndFindingsForPath(t *testing.T) {
	identifier := mustBlameProvider(t, "Kubernetes/YAML")
	resource := &goclientnew.ResourceInfo{ResourceType: "apps/v1/Deployment", ResourceName: "ns/web", ResourceNameWithoutScope: "web"}
	path := "spec.template.spec.containers.?name=web.image"

	conflicts := goclientnew.MutationConflictList{
		{Resource: resource, Path: path, Reason: "own"},
		{Resource: resource, Path: "spec.template.spec.containers.?name=web;@0", Reason: "ancestor"},
		{Resource: resource, Reason: "resource"},
		{Resource: resource, Path: "spec.template.spec.containers.?name=web.imagePullPolicy", Reason: "sibling"},
		{Resource: &goclientnew.ResourceInfo{ResourceType: "v1/Service", ResourceName: "ns/web"}, Path: path, Reason: "other resource"},
	}
	var reasons []string
	for _, conflict := range blameConflictsForPath(identifier, &conflicts, "apps/v1/Deployment", "ns/web", "web", path) {
		reasons = append(reasons, conflict.Reason)
	}
	if got := strings.Join(reasons, ","); got != "own,ancestor,resource" {
		t.Errorf("conflicts matched = %s, want own,ancestor,resource", got)
	}

	findings := goclientnew.LinkFindingList{
		{Resource: resource, Path: path, Reason: string(api.LinkFindingNeededNotFound)},
		{Resource: resource, Path: path, Reason: string(api.LinkFindingProvidedNotFound)},
	}
	matched := blameFindingsForPath(identifier, &findings, "apps/v1/Deployment", "ns/web", "web", path)
	if len(matched) != 1 || matched[0].Reason != string(api.LinkFindingNeededNotFound) {
		t.Errorf("findings matched = %+v, want only the NeededNotFound one", matched)
	}
}

func TestFindUnitPathField(t *testing.T) {
	identifier := mustBlameProvider(t, "Kubernetes/YAML")
	fields := []*Field{
		blameTestField("apps/v1/Deployment", "ns/web", "web", "metadata.name"),
		blameTestField("apps/v1/Deployment", "ns/web", "web", "spec.template.spec.containers.?name=web.image"),
		blameTestField("apps/v1/Deployment", "ns/web", "web", "spec.template.spec.containers.?name=web.name"),
		blameTestField("v1/Service", "ns/web", "web", "metadata.name"),
	}
	assignBlameRefs(identifier, fields)

	if got, err := FindField(fields, fields[1].Ref, ""); err != nil || got != fields[1] {
		t.Errorf("by REF: got %v, %v", got, err)
	}
	if got, err := FindField(fields, fields[1].Ref[:RefMinLength], ""); err != nil || got != fields[1] {
		t.Errorf("by REF prefix: got %v, %v", got, err)
	}
	if got, err := FindField(fields, "spec.template.spec.containers.?name=web;@0.image", ""); err != nil || got != fields[1] {
		t.Errorf("by anchored path: got %v, %v", got, err)
	}
	if _, err := FindField(fields, "metadata.name", ""); err == nil || !strings.Contains(err.Error(), "--resource") {
		t.Errorf("a path two resources have should ask for --resource, got %v", err)
	}
	if got, err := FindField(fields, "metadata.name", "v1/Service"); err != nil || got != fields[3] {
		t.Errorf("with --resource: got %v, %v", got, err)
	}
	if _, err := FindField(fields, "spec.template.spec.containers.?name=web", ""); err == nil ||
		!strings.Contains(err.Error(), fields[1].Ref) || !strings.Contains(err.Error(), fields[2].Ref) {
		t.Errorf("a path holding fields should list them with their REFs, got %v", err)
	}
	if _, err := FindField(fields, "spec.nothing", ""); err == nil {
		t.Error("a path the unit does not have should be an error")
	}
}

// TestAbsentConflictFields: a conflict on a path the unit has is on that field, and on a
// resource the unit has but a path it does not, or on a resource it does not have at all, is
// on a field of its own, which is absent. A conflict about a whole resource the unit does not
// have is on each of that resource's absent fields, and on a field with no path of its own only
// when nothing else names the resource.
func TestAbsentConflictFields(t *testing.T) {
	identifier := mustBlameProvider(t, "Kubernetes/YAML")
	fields := []*Field{
		blameTestField("v1/Namespace", "/staging", "staging", "metadata.name"),
		blameTestField("apps/v1/Deployment", "staging/web", "web", "metadata.name"),
		blameTestField("apps/v1/Deployment", "staging/web", "web", "spec.replicas"),
	}
	placeholder := &goclientnew.ResourceInfo{ResourceType: "v1/Namespace", ResourceName: "/confighubplaceholder", ResourceNameWithoutScope: "confighubplaceholder"}
	deployment := &goclientnew.ResourceInfo{ResourceType: "apps/v1/Deployment", ResourceName: "confighubplaceholder/web", ResourceNameWithoutScope: "web"}
	service := &goclientnew.ResourceInfo{ResourceType: "v1/Service", ResourceName: "confighubplaceholder/web", ResourceNameWithoutScope: "web"}
	conflicts := goclientnew.MutationConflictList{
		{Resource: placeholder, Reason: "UnresolvedPath"},
		{Resource: placeholder, Path: "metadata.labels.team", Reason: "UnresolvedPath"},
		{Resource: placeholder, Path: "metadata.labels.tier", Reason: "UnresolvedPath"},
		{Resource: deployment, Path: "spec.replicas", Reason: "Subtracted"},
		{Resource: deployment, Path: "spec.paused", Reason: "UnresolvedPath"},
		{Resource: service, Reason: "UnresolvedPath"},
	}

	absent := absentConflictFields(identifier, &conflicts, fields)
	var got []string
	for _, f := range absent {
		if !f.Absent {
			t.Errorf("%s %s is not marked absent", f.ResourceName, f.Path)
		}
		var reasons []string
		for _, conflict := range f.Conflicts {
			reasons = append(reasons, conflict.Reason)
		}
		got = append(got, f.ResourceType+" "+f.ResourceName+" "+f.Path+": "+strings.Join(reasons, ","))
	}
	want := []string{
		"v1/Namespace /confighubplaceholder metadata.labels.team: UnresolvedPath,UnresolvedPath",
		"v1/Namespace /confighubplaceholder metadata.labels.tier: UnresolvedPath,UnresolvedPath",
		// Named as the unit names the resource, so it is listed with the resource's other fields.
		"apps/v1/Deployment staging/web spec.paused: UnresolvedPath",
		"v1/Service confighubplaceholder/web : UnresolvedPath",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("absent fields:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}

	merged := insertAbsentFields(fields, absent)
	var order []string
	for _, f := range merged {
		order = append(order, f.ResourceName+" "+f.Path)
	}
	wantOrder := []string{
		"/staging metadata.name",
		"staging/web metadata.name",
		"staging/web spec.paused",
		"staging/web spec.replicas",
		"/confighubplaceholder metadata.labels.team",
		"/confighubplaceholder metadata.labels.tier",
		"confighubplaceholder/web ",
	}
	if strings.Join(order, "\n") != strings.Join(wantOrder, "\n") {
		t.Errorf("fields in order:\n%s\nwant:\n%s", strings.Join(order, "\n"), strings.Join(wantOrder, "\n"))
	}
}
