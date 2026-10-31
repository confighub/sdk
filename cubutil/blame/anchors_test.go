// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package blame

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/confighub/sdk/core/configkit/yamlkit"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/confighub/sdk/core/third_party/gaby"
)

const externalSecretClone = `apiVersion: external-secrets.io/v1
kind: ExternalSecret
metadata:
  name: app
  namespace: default
spec:
  data:
  - secretKey: user
    remoteRef:
      key: db-user
  - secretKey: password
    remoteRef:
      key: db-password
`

// externalSecretUnit is a unit cloned with externalSecretClone, in which someone then changed
// and protected the remote key of the second element of spec.data -- a list with no merge key,
// so the record names the element by its content. It returns the unit's data, its record, and
// the path the record keys the change by.
func externalSecretUnit(t *testing.T) (string, goclientnew.ResourceMutationList, string) {
	t.Helper()
	provider := mustBlameProvider(t, "Kubernetes/YAML")
	edited := strings.Replace(externalSecretClone, "key: db-password", "key: db-password-v2", 1)
	before, err := gaby.ParseAll([]byte(externalSecretClone))
	if err != nil {
		t.Fatal(err)
	}
	after, err := gaby.ParseAll([]byte(edited))
	if err != nil {
		t.Fatal(err)
	}
	clone, err := yamlkit.ComputeMutations(gaby.Container{}, before, 1, provider)
	if err != nil {
		t.Fatal(err)
	}
	edit, err := yamlkit.ComputeMutations(before, after, 5, provider)
	if err != nil {
		t.Fatal(err)
	}
	if len(edit) != 1 || len(edit[0].PathMutationMap) != 1 {
		t.Fatalf("expected one changed path, got %+v", edit)
	}
	// The record keys the path as AddMutations stores it, without its positional fallback.
	var stored string
	for path, info := range edit[0].PathMutationMap {
		stored = string(yamlkit.CanonicalMutationPath(path))
		info.Protected = true
		edit[0].PathMutationMap[path] = info
	}
	if !strings.Contains(stored, "spec.data.?~") {
		t.Fatalf("expected the element to be named by its content, got %s", stored)
	}
	record, _ := yamlkit.AddMutations(provider, clone, edit)

	raw, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	var sources goclientnew.ResourceMutationList
	if err := json.Unmarshal(raw, &sources); err != nil {
		t.Fatal(err)
	}
	return edited, sources, stored
}

// TestBlameCreditsAContentIdentityPath: the field is listed by position, spec.data.1, and the
// record names it by content. Resolved against the data, the change and its protection are
// the field's, rather than the clone's resource-level entry and no protection.
func TestBlameCreditsAContentIdentityPath(t *testing.T) {
	data, sources, _ := externalSecretUnit(t)
	fields, docs, err := flattenUnitFields(data, "Kubernetes/YAML")
	if err != nil {
		t.Fatal(err)
	}
	identifier := mustBlameProvider(t, "Kubernetes/YAML")
	var field *Field
	for _, f := range fields {
		if f.Path == "spec.data.1.remoteRef.key" {
			field = f
		}
	}
	if field == nil {
		t.Fatalf("no field spec.data.1.remoteRef.key among %d", len(fields))
	}

	info, protected := creditPath(identifier, sources, field.ResourceType, field.ResourceName, field.resourceNameCore, field.storedPath)
	if info == nil || info.Index != 1 || protected {
		t.Fatalf("unresolved, the record should credit the clone's resource-level entry (1), unprotected; got %+v %v", info, protected)
	}

	resolved := resolveRecordAnchors(docs, sources, nil)
	info, protected = creditPath(identifier, resolved, field.ResourceType, field.ResourceName, field.resourceNameCore, field.storedPath)
	if info == nil || info.Index != 5 || !protected {
		t.Fatalf("resolved, the field should be credited to the edit (5) and protected; got %+v %v", info, protected)
	}

	// The other element was not changed, and is still the clone's.
	info, protected = creditPath(identifier, resolved, field.ResourceType, field.ResourceName, field.resourceNameCore, "spec.data.0.remoteRef.key")
	if info == nil || info.Index != 1 || protected {
		t.Fatalf("an unchanged element should be credited to the clone (1), unprotected; got %+v %v", info, protected)
	}
}

// TestBlameMatchesContentIdentityConflictsAndGuards: what the unit records about a field under
// a content-identity path -- a dropped upstream change, a guard -- is about the field it
// resolves to.
func TestBlameMatchesContentIdentityConflictsAndGuards(t *testing.T) {
	data, _, stored := externalSecretUnit(t)
	_, docs, err := flattenUnitFields(data, "Kubernetes/YAML")
	if err != nil {
		t.Fatal(err)
	}
	identifier := mustBlameProvider(t, "Kubernetes/YAML")
	resource := &goclientnew.ResourceInfo{
		ResourceType:             "external-secrets.io/v1/ExternalSecret",
		ResourceName:             "default/app",
		ResourceNameWithoutScope: "app",
	}
	conflicts := goclientnew.MutationConflictList{{Resource: resource, Path: stored, Reason: "ProtectedPath"}}
	annotations := goclientnew.PathAnnotationList{{
		Resource:          resource,
		PathAnnotationMap: map[string]goclientnew.PathAnnotations{stored: {"Guard": {"owner": "secrets-team"}}},
	}}
	unit := resolveUnitAnchors(docs, &goclientnew.Unit{Conflicts: &conflicts, PathAnnotations: &annotations})

	matched := blameConflictsForPath(identifier, unit.Conflicts, resource.ResourceType, resource.ResourceName, "app", "spec.data.1.remoteRef.key")
	if len(matched) != 1 {
		t.Fatalf("the conflict should be on spec.data.1.remoteRef.key, got %+v", matched)
	}
	if matched = blameConflictsForPath(identifier, unit.Conflicts, resource.ResourceType, resource.ResourceName, "app", "spec.data.0.remoteRef.key"); len(matched) != 0 {
		t.Fatalf("the conflict should not be on the other element, got %+v", matched)
	}
	if conflicts[0].Path != stored {
		t.Fatal("the unit handed in should be left as it was")
	}
	guards := blameGuardsForPath(identifier, *unit.PathAnnotations, resource.ResourceType, resource.ResourceName, "app", "spec.data.1.remoteRef.key")
	if guards["owner"] != "secrets-team" {
		t.Fatalf("the guard should be on spec.data.1.remoteRef.key, got %+v", guards)
	}
}

// TestResolveRecordAnchorsFoldsKeepingProtection: two entries resolving to one field are one,
// credited to the later change and protected if either was.
func TestResolveRecordAnchorsFoldsKeepingProtection(t *testing.T) {
	data, sources, stored := externalSecretUnit(t)
	_, docs, err := flattenUnitFields(data, "Kubernetes/YAML")
	if err != nil {
		t.Fatal(err)
	}
	update := goclientnew.Update
	(*sources[0].PathMutationMap)["spec.data.1.remoteRef.key"] = goclientnew.MutationInfo{MutationType: &update, Index: 7}
	resolved := resolveRecordAnchors(docs, sources, nil)
	if _, ok := (*sources[0].PathMutationMap)[stored]; !ok {
		t.Fatal("the record handed in should be left as it was")
	}
	info, ok := (*resolved[0].PathMutationMap)["spec.data.1.remoteRef.key"]
	if !ok || info.Index != 7 || !info.Protected {
		t.Fatalf("expected the later change (7), protected, got %+v", info)
	}
	if len(*resolved[0].PathMutationMap) != len(*sources[0].PathMutationMap)-1 {
		t.Fatalf("expected the two entries folded into one, got %+v", *resolved[0].PathMutationMap)
	}
}

// TestCreditPathResourceProtectedAsAWhole: every field of a resource protected as a whole is
// protected, whatever its own entry says, since a merge withholds the whole resource first.
func TestCreditPathResourceProtectedAsAWhole(t *testing.T) {
	add := goclientnew.Add
	pathMutations := goclientnew.MutationMap{"metadata.annotations.owner": {MutationType: &add, Index: 4}}
	sources := goclientnew.ResourceMutationList{{
		Resource:             &goclientnew.ResourceInfo{ResourceType: "v1/ServiceAccount", ResourceName: "default/app"},
		ResourceMutationInfo: &goclientnew.MutationInfo{MutationType: &add, Index: 3, Protected: true},
		PathMutationMap:      &pathMutations,
	}}
	for _, path := range []string{"metadata.name", "metadata.annotations.owner"} {
		info, protected := creditPath(nil, sources, "v1/ServiceAccount", "default/app", "app", path)
		if info == nil || !protected {
			t.Fatalf("%s should be protected with its resource, got %+v %v", path, info, protected)
		}
	}
	if !resourceProtected(nil, sources, "v1/ServiceAccount", "default/app", "app") {
		t.Fatal("the resource should read as protected as a whole")
	}
}

// TestBlameFollowsAChangeThroughTheList: one change removed the first element of spec.data and
// edited the rest, so its entries name elements by content that no longer exists, between
// neighbors that do not either. Followed through that change, each is credited to the field it
// changed, which has moved up one.
func TestBlameFollowsAChangeThroughTheList(t *testing.T) {
	provider := mustBlameProvider(t, "Kubernetes/YAML")
	header := "apiVersion: external-secrets.io/v1\nkind: ExternalSecret\nmetadata:\n  name: env\n  namespace: default\nspec:\n  data:\n"
	element := func(name, key string) string {
		return "  - secretKey: " + name + "\n    remoteRef:\n      key: " + key + "\n"
	}
	before := header + element("A", "/old/a") + element("B", "/old/b") + element("C", "/old/c")
	after := header + element("B", "/new/b") + element("C", "/new/c")
	beforeDocs, err := gaby.ParseAll([]byte(before))
	if err != nil {
		t.Fatal(err)
	}
	afterDocs, err := gaby.ParseAll([]byte(after))
	if err != nil {
		t.Fatal(err)
	}
	clone, err := yamlkit.ComputeMutations(gaby.Container{}, beforeDocs, 1, provider)
	if err != nil {
		t.Fatal(err)
	}
	edit, err := yamlkit.ComputeMutations(beforeDocs, afterDocs, 4, provider)
	if err != nil {
		t.Fatal(err)
	}
	record, _ := yamlkit.AddMutations(provider, clone, edit)
	raw, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	var sources goclientnew.ResourceMutationList
	if err := json.Unmarshal(raw, &sources); err != nil {
		t.Fatal(err)
	}

	fields, docs, err := flattenUnitFields(after, "Kubernetes/YAML")
	if err != nil {
		t.Fatal(err)
	}
	_, beforeResourceDocs, err := flattenUnitFields(before, "Kubernetes/YAML")
	if err != nil {
		t.Fatal(err)
	}
	change := func(mutationNum int64) (*resourceDocs, *resourceDocs) {
		if mutationNum != 4 {
			return nil, nil
		}
		return beforeResourceDocs, docs
	}
	if unplaced := resolveRecordAnchors(docs, sources, nil); !recordHasAnchors(unplaced) {
		t.Fatal("without the change, the entries should not be placed")
	}
	resolved := resolveRecordAnchors(docs, sources, change)
	for _, f := range fields {
		if !strings.HasSuffix(f.Path, ".remoteRef.key") {
			continue
		}
		info, _ := creditPath(provider, resolved, f.ResourceType, f.ResourceName, f.resourceNameCore, f.storedPath)
		if info == nil || info.Index != 4 || !strings.HasPrefix(info.Value, "/new/") {
			t.Errorf("%s (%s) should be credited to the change (4), got %+v", f.Path, f.Value, info)
		}
	}
}
