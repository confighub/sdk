// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"strings"

	"github.com/confighub/sdk/core/cubapi"
	"github.com/confighub/sdk/core/function/api"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

// displayWriteConflicts reports what a write to a unit could not apply: what a merge withheld,
// or a guard its clearance did not cover. A write that withheld part of a change would
// otherwise read as having landed whole. It names the command that acts on them, with the
// clearance that covers the guarded ones. Printed only with the command's default output, and
// not with --quiet.
//
// space is the unit's space slug, or its ID where the response names no slug.
func displayWriteConflicts(space, unit string, conflicts *goclientnew.MutationConflictList, dryRun bool) {
	if conflicts == nil || len(*conflicts) == 0 || !showProgress() {
		return
	}
	withheld := *conflicts
	count := fmt.Sprintf("%d %s", len(withheld), plural("change", len(withheld)))
	tprintRaw("")
	if dryRun {
		tprint("Would withhold %s on unit %s:", count, unit)
	} else {
		tprint("Withheld %s on unit %s:", count, unit)
	}
	displayConflicts(withheld)

	guarded := 0
	for i := range withheld {
		if api.ConflictReasonHasGuards(api.ConflictReason(withheld[i].Reason)) {
			guarded++
		}
	}
	if dryRun {
		// The change is run again, so the clearance restates the default it was sent with.
		if clearance := clearanceFlags(clearanceToRetry(withheld)); clearance != "" {
			tprint("Pass %s to clear their guards.", clearance)
		}
		return
	}
	// Applying the conflicts writes only what was withheld, so what covers their guards is enough.
	clearance := clearanceFlags(cubapi.ClearanceCoveringGuards(withheld))
	conflictsCmd := fmt.Sprintf("cub unit conflicts --space %s %s", space, unit)
	switch {
	case guarded == len(withheld) && clearance != "":
		tprint("Apply them with: %s --apply %s", conflictsCmd, clearance)
	case clearance != "":
		tprint("Review them with: %s", conflictsCmd)
		tprint("Applying the guarded ones needs %s.", clearance)
	default:
		tprint("Review them with: %s", conflictsCmd)
	}
}

// clearanceFlags renders a clearance as the --clearance flags that state it, one per requirement.
func clearanceFlags(clearance goclientnew.Clearance) string {
	flags := make([]string, 0, len(clearance))
	for _, requirement := range clearance {
		flags = append(flags, fmt.Sprintf("--clearance %q", formatClearance(&goclientnew.Clearance{requirement})))
	}
	return strings.Join(flags, " ")
}
