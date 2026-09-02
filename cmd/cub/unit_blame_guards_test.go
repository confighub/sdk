// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"testing"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

// blame reports guards by converting the stored table to the SDK type and asking the same
// lookup the merge engine asks. What is worth testing here is that the conversion carries the
// parts the lookup reads -- a conversion that quietly dropped ResourceAnnotations, or keyed the
// path map wrong, would report "no reasons" for a field that has them, which is the answer a
// reader would act on.

func guardAnnotationTable(resourceName string, paths map[string]map[string]string,
	resourceLevel map[string]string) goclientnew.PathAnnotationList {
	entry := goclientnew.ResourcePathAnnotations{
		Resource: &goclientnew.ResourceInfo{
			ResourceType:             "apps/v1/Deployment",
			ResourceName:             resourceName,
			ResourceNameWithoutScope: "web",
		},
		PathAnnotationMap: map[string]goclientnew.PathAnnotations{},
	}
	for path, guards := range paths {
		entry.PathAnnotationMap[path] = goclientnew.PathAnnotations{"Guard": guards}
	}
	if resourceLevel != nil {
		annotations := goclientnew.PathAnnotations{"Guard": resourceLevel}
		entry.ResourceAnnotations = &annotations
	}
	return goclientnew.PathAnnotationList{entry}
}

func TestBlameGuardsForPathFindsAPathsOwnEntry(t *testing.T) {
	table := guardAnnotationTable("ns/web", map[string]map[string]string{
		"spec.replicas": {"policy-exception": "capacity"},
	}, nil)

	got := blameGuardsForPath(table, "apps/v1/Deployment", "ns/web", "web", "spec.replicas")
	if got["policy-exception"] != "capacity" {
		t.Errorf("expected the path's own guard, got %v", got)
	}
	if guards := blameGuardsForPath(table, "apps/v1/Deployment", "ns/web", "web", "spec.paused"); guards != nil {
		t.Errorf("an unguarded path should report nothing, got %v", guards)
	}
}

func TestBlameGuardsForPathInheritsFromAnAncestorAndTheResource(t *testing.T) {
	table := guardAnnotationTable("ns/web", map[string]map[string]string{
		"spec.template.spec": {"owner": "platform"},
	}, map[string]string{"policy-exception": "pinned"})

	// A field inside the guarded subtree carries both the subtree's reason and the resource's,
	// which is what an operation writing there has to be cleared for.
	got := blameGuardsForPath(table, "apps/v1/Deployment", "ns/web", "web",
		"spec.template.spec.containers.?name=app.image")
	if got["owner"] != "platform" {
		t.Errorf("expected the ancestor's guard to be inherited, got %v", got)
	}
	if got["policy-exception"] != "pinned" {
		t.Errorf("expected the resource-level guard to be inherited, got %v", got)
	}

	// A field outside the subtree still carries the resource's.
	outside := blameGuardsForPath(table, "apps/v1/Deployment", "ns/web", "web", "spec.replicas")
	if outside["policy-exception"] != "pinned" {
		t.Errorf("expected the resource-level guard outside the subtree, got %v", outside)
	}
	if _, found := outside["owner"]; found {
		t.Errorf("a subtree's guard must not reach outside it, got %v", outside)
	}
}

func TestBlameGuardsForPathMatchesARenamedResource(t *testing.T) {
	// A variant that renamed the resource still carries the guards written under the old name,
	// which is the same fallback matchBlameResource makes for the mutation record.
	table := guardAnnotationTable("ns/renamed", map[string]map[string]string{
		"spec.replicas": {"owner": "platform"},
	}, nil)

	got := blameGuardsForPath(table, "apps/v1/Deployment", "other/web", "web", "spec.replicas")
	if got["owner"] != "platform" {
		t.Errorf("expected the unscoped name to match, got %v", got)
	}

	none := blameGuardsForPath(table, "apps/v1/Deployment", "other/api", "api", "spec.replicas")
	if none != nil {
		t.Errorf("a different resource must not pick up another's guards, got %v", none)
	}
}

func TestBlameGuardsForPathIsEmptyForAnUnguardedUnit(t *testing.T) {
	if got := blameGuardsForPath(nil, "apps/v1/Deployment", "ns/web", "web", "spec.replicas"); got != nil {
		t.Errorf("a Unit with no annotations should report nothing, got %v", got)
	}
}

func TestFormatBlameGuardsIsSortedAndFlagShaped(t *testing.T) {
	// Rendered the way --guard takes them, and sorted, so a listing is diffable between runs.
	got := formatBlameGuards(map[string]string{"owner": "platform", "policy-exception": "pinned"})
	if got != "owner=platform policy-exception=pinned" {
		t.Errorf("unexpected rendering: %q", got)
	}
	if formatBlameGuards(nil) != "" {
		t.Error("no guards should render as nothing at all")
	}
}
