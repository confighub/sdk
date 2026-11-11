// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package cubapi

import (
	"testing"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
)

func TestUnitsByChangeSet(t *testing.T) {
	opening, other, third := uuid.New(), uuid.New(), uuid.New()
	here, elsewhere := uuid.New(), uuid.New()
	nilID := uuid.Nil
	unit := func(slug string, space goclientnew.UUID, cs *goclientnew.UUID) *goclientnew.ExtendedUnit {
		eu := &goclientnew.ExtendedUnit{Unit: &goclientnew.Unit{Slug: slug, SpaceID: space, ChangeSetID: cs}}
		if space == elsewhere {
			eu.Space = &goclientnew.Space{Slug: "platform"}
		}
		return eu
	}
	units := []*goclientnew.ExtendedUnit{
		unit("free", here, nil),
		unit("zeroed", here, &nilID),
		unit("already", here, &opening),
		unit("web", here, &other),
		unit("api", here, &other),
		unit("config", elsewhere, &third),
	}
	already, held := unitsByChangeSet(units, opening, here)
	if already != 1 {
		t.Errorf("already = %d, want 1: only the unit in the changeset being opened", already)
	}
	if len(held) != 2 || len(held[other]) != 2 || len(held[third]) != 1 {
		t.Fatalf("held = %v, want two units under %s and one under %s", held, other, third)
	}
	if got := heldUnitCount(held); got != 3 {
		t.Errorf("heldUnitCount = %d, want 3", got)
	}
	// A unit outside the opener's Space is named with its Space.
	names := map[uuid.UUID]string{other: "capacity-1 (Open)", third: "platform/hotfix (Open)"}
	want := "api, web are in capacity-1 (Open); platform/config is in platform/hotfix (Open)"
	if got := describeHeldUnits(held, names); got != want {
		t.Errorf("describeHeldUnits = %q, want %q", got, want)
	}
	if got := (&ChangeSetHeldError{Held: 3, Description: want}).Error(); got != "another changeset holds 3 of them: "+want {
		t.Errorf("ChangeSetHeldError = %q", got)
	}
}

func TestUnitsByChangeSetNothingHeld(t *testing.T) {
	opening, space := uuid.New(), uuid.New()
	units := []*goclientnew.ExtendedUnit{
		{Unit: &goclientnew.Unit{Slug: "a", SpaceID: space}},
		{Unit: &goclientnew.Unit{Slug: "b", SpaceID: space, ChangeSetID: &opening}},
	}
	already, held := unitsByChangeSet(units, opening, space)
	if already != 1 || len(held) != 0 {
		t.Errorf("already, held = %d, %v, want 1 and none", already, held)
	}
}
