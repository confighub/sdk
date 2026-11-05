// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"testing"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
)

func TestUnitsByChangeSet(t *testing.T) {
	opening, other, third := uuid.New(), uuid.New(), uuid.New()
	nilID := uuid.Nil
	units := []*goclientnew.Unit{
		{Slug: "free"},
		{Slug: "zeroed", ChangeSetID: &nilID},
		{Slug: "already", ChangeSetID: &opening},
		{Slug: "web", ChangeSetID: &other},
		{Slug: "api", ChangeSetID: &other},
		{Slug: "config", ChangeSetID: &third},
	}
	already, held := unitsByChangeSet(units, opening)
	if already != 1 {
		t.Errorf("already = %d, want 1: only the unit in the changeset being opened", already)
	}
	if len(held) != 2 || len(held[other]) != 2 || len(held[third]) != 1 {
		t.Fatalf("held = %v, want two units under %s and one under %s", held, other, third)
	}
	if got := heldUnitCount(held); got != 3 {
		t.Errorf("heldUnitCount = %d, want 3", got)
	}
	names := map[uuid.UUID]string{other: "capacity-1 (Open)", third: "platform/hotfix (Open)"}
	want := "api, web are in capacity-1 (Open); config is in platform/hotfix (Open)"
	if got := describeHeldUnits(held, names); got != want {
		t.Errorf("describeHeldUnits = %q, want %q", got, want)
	}
}

func TestUnitsByChangeSetNothingHeld(t *testing.T) {
	opening := uuid.New()
	already, held := unitsByChangeSet([]*goclientnew.Unit{{Slug: "a"}, {Slug: "b", ChangeSetID: &opening}}, opening)
	if already != 1 || len(held) != 0 {
		t.Errorf("already, held = %d, %v, want 1 and none", already, held)
	}
}
