// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"testing"

	"github.com/confighub/sdk/cmd/cub/upload"
	"github.com/confighub/sdk/core/reconcile"
)

// TestUploadDesiredUnitsAppConfigPlaceholderIsBodyless pins the subtlety that
// makes AppConfig survive a re-upload. The placeholder Unit's content comes
// from the Upsert link, not from this command. If it were handed to the engine
// as a normal desired Unit it would be diffed against an empty body and
// "updated" on every re-upload, wiping what the link rendered into it; if it
// were omitted entirely it would look like a Unit that fell out of the input
// and --prune would empty it. Bodyless is the only correct answer: present, but
// never written.
func TestUploadDesiredUnitsAppConfigPlaceholderIsBodyless(t *testing.T) {
	plan := &upload.Plan{Units: []upload.Unit{
		{
			Slug:      "app-config",
			Toolchain: "AppConfig/Properties",
			Content:   "key=value\n",
			Kind:      upload.UnitAppConfig,
			AppConfig: &upload.AppConfigManifest{
				CarrierName: "app-config",
				Toolchain:   "AppConfig/Properties",
			},
		},
	}}
	a := &variantUploadOptions{sourceDesc: "oci://ghcr.io/org/bundle"}

	got := uploadDesiredUnits(plan, a)
	if len(got) != 2 {
		t.Fatalf("an AppConfig manifest should contribute 2 desired Units, got %d: %+v", len(got), got)
	}

	data, placeholder := got[0], got[1]
	if data.Slug != "app-config" {
		t.Errorf("data Unit slug = %q, want %q", data.Slug, "app-config")
	}
	if data.Bodyless {
		t.Error("the AppConfig data Unit must not be Bodyless; it is what changes between uploads")
	}
	if string(data.Content) != "key=value\n" {
		t.Errorf("data Unit content = %q, want the rendered body", data.Content)
	}
	if data.Toolchain != "AppConfig/Properties" {
		t.Errorf("data Unit toolchain = %q, want the AppConfig toolchain", data.Toolchain)
	}

	if placeholder.Slug != "app-config-rendered" {
		t.Errorf("placeholder slug = %q, want %q", placeholder.Slug, "app-config-rendered")
	}
	if !placeholder.Bodyless {
		t.Error("the placeholder must be Bodyless: its body is produced by the Upsert link, so " +
			"writing to it would clobber the rendered ConfigMap")
	}
	if placeholder.Content != nil {
		t.Errorf("placeholder content = %q, want nil so the engine never stages a body for it", placeholder.Content)
	}
}

// TestUploadDesiredUnitsCarriesSourceIdentity checks that every body-carrying
// Unit re-uses the upload's source description as its merge-external-source
// identity. That value is what the first upload stamped, and keeping it stable
// is what makes an update resolve against the previous external push rather
// than against an operator's edit.
func TestUploadDesiredUnitsCarriesSourceIdentity(t *testing.T) {
	plan := &upload.Plan{Units: []upload.Unit{
		{Slug: "web", Toolchain: "Kubernetes/YAML", Content: "a", Kind: upload.UnitNormal},
		{Slug: "web-crds", Toolchain: "Kubernetes/YAML", Content: "b", Kind: upload.UnitCRD},
	}}
	a := &variantUploadOptions{sourceDesc: "oci://ghcr.io/org/bundle"}

	for _, u := range uploadDesiredUnits(plan, a) {
		if u.SourceName != "oci://ghcr.io/org/bundle" {
			t.Errorf("Unit %q SourceName = %q, want the upload's source description",
				u.Slug, u.SourceName)
		}
	}
}

// TestDropDeletes covers the safety default: a Unit that vanished from the
// input is reported, not emptied, unless --prune was passed.
func TestDropDeletes(t *testing.T) {
	newPlan := func() reconcile.Plan {
		return reconcile.Plan{Spaces: []reconcile.SpacePlan{{
			Space:   "web-base",
			Adds:    []reconcile.SlugDiff{{Slug: "added"}},
			Deletes: []reconcile.SlugDiff{{Slug: "gone"}, {Slug: "also-gone"}},
		}}}
	}

	t.Run("without prune the deletes are stripped and reported", func(t *testing.T) {
		p := newPlan()
		skipped := dropDeletes(&p, false)
		if len(p.Spaces[0].Deletes) != 0 {
			t.Errorf("deletes = %+v, want none retained without --prune", p.Spaces[0].Deletes)
		}
		if len(skipped) != 2 {
			t.Errorf("skipped = %v, want both vanished Units reported", skipped)
		}
		if len(p.Spaces[0].Adds) != 1 {
			t.Error("dropping deletes must not disturb the adds")
		}
	})

	t.Run("with prune the deletes survive and nothing is reported as skipped", func(t *testing.T) {
		p := newPlan()
		skipped := dropDeletes(&p, true)
		if len(p.Spaces[0].Deletes) != 2 {
			t.Errorf("deletes = %+v, want both retained under --prune", p.Spaces[0].Deletes)
		}
		if skipped != nil {
			t.Errorf("skipped = %v, want nil: nothing was withheld", skipped)
		}
	})
}

// TestDropDeletesLeavesNoChangesWhenOnlyDeletes guards the no-op path: a
// re-upload whose only difference is a removed Unit must report "already up to
// date" rather than opening a ChangeSet, once the deletes are withheld.
func TestDropDeletesLeavesNoChangesWhenOnlyDeletes(t *testing.T) {
	p := reconcile.Plan{Spaces: []reconcile.SpacePlan{{
		Space:   "web-base",
		Deletes: []reconcile.SlugDiff{{Slug: "gone"}},
	}}}
	if !p.HasChanges() {
		t.Fatal("precondition: a plan with a delete has changes")
	}
	dropDeletes(&p, false)
	if p.HasChanges() {
		t.Error("after withholding the only delete the plan must have no changes, " +
			"so the re-upload is a genuine no-op")
	}
}
