// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"slices"
	"testing"
	"time"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
)

func TestGroupSimilarGrants(t *testing.T) {
	prod, preview := uuid.New(), uuid.New()
	manage := []cubapi.GrantedAction{{Action: "Manage", Direct: true}}
	edit := []cubapi.GrantedAction{{Action: "Edit", Direct: true}}
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	tag := func(space goclientnew.UUID, name string, day int, actions []cubapi.GrantedAction) cubapi.Grant {
		return cubapi.Grant{EntityType: "Tag", ID: uuid.New(), SpaceID: space, Name: name, CreatedAt: start.AddDate(0, 0, day), Actions: actions}
	}
	names := func(grants []cubapi.Grant) []string {
		var out []string
		for _, grant := range grants {
			out = append(out, grant.Name)
		}
		return out
	}

	// Listed by name, created out of that order: the newest are release-2, release-10 and release-11.
	grants := []cubapi.Grant{
		tag(prod, "release-1", 1, manage),
		tag(prod, "release-2", 9, manage),
		tag(prod, "release-3", 2, manage),
		tag(prod, "release-4", 3, manage),
		tag(prod, "pinned", 4, edit),
		tag(prod, "release-10", 10, manage),
		tag(prod, "release-11", 11, manage),
		tag(preview, "release-1", 5, manage),
		tag(preview, "release-2", 6, manage),
		tag(preview, "release-3", 7, manage),
		tag(preview, "release-4", 8, manage),
	}
	groups := groupSimilarGrants(grants)
	if len(groups) != 3 {
		t.Fatalf("got %d groups, want 3: Manage in prod, Edit in prod, and Manage in preview", len(groups))
	}

	shown, hidden := newestGrants(groups[0])
	if want := []string{"release-2", "release-10", "release-11"}; !slices.Equal(names(shown), want) || hidden != 3 {
		t.Errorf("Manage in prod: listed %v and counted %d, want %v and 3", names(shown), hidden, want)
	}

	// A grant that gives something else is not counted with the rest.
	shown, hidden = newestGrants(groups[1])
	if want := []string{"pinned"}; !slices.Equal(names(shown), want) || hidden != 0 {
		t.Errorf("Edit in prod: listed %v and counted %d, want %v and 0", names(shown), hidden, want)
	}

	// One left out would take the line its count does, so all four are listed.
	shown, hidden = newestGrants(groups[2])
	if len(shown) != 4 || hidden != 0 {
		t.Errorf("Manage in preview: listed %d and counted %d, want 4 and 0", len(shown), hidden)
	}
}
