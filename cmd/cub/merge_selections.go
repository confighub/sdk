// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"strings"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

// displayMergeSelections says what an upgrade or resolve that named the revisions to take took,
// or with --dry-run would take, over each link -- or a change order's promotion that took its
// range out of order or overrode skips in it: the revisions, the skips that overrode, and the
// earlier pending revisions that wrote some of the same paths, which taking a revision ahead of
// them applies its change without.
func displayMergeSelections(unit string, selections []goclientnew.MergeSelection, dryRun bool) {
	if quiet || isAlternativeOutput() {
		return
	}
	taken, took := "is taken", "took"
	if dryRun {
		taken, took = "would be taken", "would take"
	}
	for _, selection := range selections {
		switch {
		case len(selection.Taken) == 0 && len(selection.DeclinedHeld) == 0:
			tprint("Unit %s %s no upstream revisions: it has merged them all already", unit, took)
			continue
		case len(selection.Taken) == 0:
			tprint("Unit %s %s no upstream revisions", unit, took)
		default:
			tprint("Unit %s %s upstream %s %s", unit, took, plural("revision", len(selection.Taken)), revisionNumRuns(selection.Taken))
		}
		for _, skip := range selection.SkipsOverridden {
			if skip.Reason != "" {
				tprint("  revision %d was skipped: %s; taking it removes the skip", skip.RevisionNum, skip.Reason)
			} else {
				tprint("  revision %d was skipped; taking it removes the skip", skip.RevisionNum)
			}
		}
		for _, held := range selection.DeclinedHeld {
			reason := ""
			if held.Reason != "" {
				reason = ": " + held.Reason
			}
			tprint("  revision %d was declined%s; it is not taken until its skip is made an ordinary one (cub link skip --decline=false) or removed",
				held.RevisionNum, reason)
		}
		for _, overlap := range selection.Overlaps {
			what := "it as a whole"
			if len(overlap.Paths) > 0 {
				what = strings.Join(overlap.Paths, ", ")
			}
			tprint("  revision %d %s ahead of pending revision %d, which also wrote %s of %s",
				overlap.RevisionNum, taken, overlap.PendingRevisionNum, what, overlap.ResourceName)
		}
	}
}
