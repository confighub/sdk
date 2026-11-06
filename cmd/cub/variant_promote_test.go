// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

// The promote summary counts the units upgraded, added, emptied, and revived, and leaves the rest
// to --verbose. A unit that already had the change is not counted as upgraded.
func TestDisplayPromoteSpaceSummary(t *testing.T) {
	upstream := uuid.New()
	space := &goclientnew.PromoteSpaceResult{
		SpaceSlug:       "prod",
		UpstreamSpaceID: &upstream,
		Units: []goclientnew.PromoteUnitResult{
			{Slug: "upgraded", Action: promoteUnitActionUpgrade},
			{Slug: "had-it", Action: promoteUnitActionUpgrade, Reason: "NoChange"},
			{Slug: "emptied", Action: promoteUnitActionEmpty},
			{Slug: "revived", Action: promoteUnitActionRevive},
			{Slug: "added", Action: promoteUnitActionClone},
			{Slug: "marked", Action: promoteUnitActionMark},
			{Slug: "outside", Action: promoteUnitActionSkip, Reason: "CreatedAfterChangeOrder"},
		},
		Links: []goclientnew.PromoteLinkResult{
			{Slug: "copied", Action: promoteLinkActionCreate},
			{Slug: "orphan", Action: promoteLinkActionOrphaned},
		},
	}
	summary := []string{
		"Upgraded 3 unit(s) behind their upstream",
		"  1 of them emptied, as their upstream units were",
		"  1 of them revived, as their upstream units have content again",
		"Adding 1 unit(s) from upstream",
	}
	details := []string{
		"  + added",
		"1 unit(s) already had the change, and were marked without a new revision",
		"Marked 1 unit(s) the change order covers and carries no changes for",
		"Leaving 1 upstream unit(s) outside the change order uncloned; promote without --change-order to add them:",
		"  - outside",
		"Copied 1 link(s) from upstream",
		"Leaving 1 link(s) whose upstream link was deleted:",
		"  - orphan",
	}

	saved := verbose
	t.Cleanup(func() { verbose = saved })
	for _, v := range []bool{false, true} {
		verbose = v
		want := summary
		if v {
			want = append(append([]string{}, summary...), details...)
		}
		got := captureStdout(t, func() { displayPromoteSpaceSummary(space, false, false) })
		if got != strings.Join(want, "\n")+"\n" {
			t.Errorf("verbose=%v: got\n%s\nwant\n%s", v, got, strings.Join(want, "\n"))
		}
	}
}
