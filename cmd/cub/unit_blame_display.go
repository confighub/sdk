// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/olekukonko/tablewriter"
)

// blameMaxValueWidth keeps a long value -- a probe body, a base64 blob -- from
// pushing the columns that answer the question off the screen. --verbose prints the
// value whole underneath instead.
const blameMaxValueWidth = 40

// displayBlame renders the field table, grouped by resource. One line per field:
// what is there, and the change that put it there after following upstream. With
// --verbose each field also gets its change description and the hops in between,
// which is the same information the columns summarize.
func displayBlame(fields []*blameField) {
	if len(fields) == 0 {
		tprintRaw("No fields")
		return
	}

	resource := ""
	var table *tablewriter.Table
	flush := func() {
		if table != nil {
			table.Render()
			table = nil
		}
	}

	for _, f := range fields {
		if key := f.ResourceType + " " + f.ResourceName; key != resource {
			flush()
			resource = key
			tprintRaw("")
			tprintRaw(fmt.Sprintf("%sResource: %s%s", colorLightBlue, resource, colorReset))
			table = tableView()
			if !noheader {
				table.SetHeader([]string{"", "Path", "Value", "Set By", "Where", "Rev", "When"})
			}
		}

		origin := f.Origin()
		// One column for both statements about a field, because they answer the same
		// question at different resolutions and a field can carry both: "*" says a merge
		// leaves this alone, "!" says there are recorded reasons an operation must be
		// cleared for. The reasons themselves are in --verbose, since a key=value pair does
		// not fit a marker and a table wide enough for it would push out the columns that
		// answer the question asked.
		mark := ""
		if f.Protected {
			mark += "*"
		}
		if len(f.Guards) > 0 {
			mark += "!"
		}
		setBy, whereSlug, rev, when := "", "", "", ""
		if origin != nil {
			setBy = origin.SetBy
			whereSlug = origin.SpaceSlug
			if origin.RevisionNum != 0 {
				rev = fmt.Sprintf("%d", origin.RevisionNum)
			}
			when = blameAgo(origin.When)
		}
		table.Append([]string{
			mark,
			f.Path,
			truncateWithEllipsis(blameOneLine(f.Value), blameMaxValueWidth),
			setBy,
			whereSlug,
			rev,
			when,
		})

		if verbose {
			flush()
			displayBlameDetail(f)
		}
	}
	flush()

	if !noheader {
		tprintRaw("")
		tprintRaw(fmt.Sprintf("%s* a protected local override: a merge from upstream leaves it alone%s",
			colorDim, colorReset))
		if anyGuarded(fields) {
			tprintRaw(fmt.Sprintf("%s! guarded: reasons are recorded for this value, and an operation must be cleared for them before overwriting it (--verbose to see them)%s",
				colorDim, colorReset))
		}
	}
}

// displayBlameDetail prints one field's full record: the value as it stands, and
// every hop from this unit out to where the value was set, each with who made the
// change and what they called it.
func displayBlameDetail(f *blameField) {
	// The reasons first: they are what an operation about to write here has to know, and
	// unlike the chain they are about the value now rather than about how it got here.
	if len(f.Guards) > 0 {
		tprintRaw(fmt.Sprintf("    %sguarded: %s%s", colorDim, formatBlameGuards(f.Guards), colorReset))
	}
	for i, origin := range f.Chain {
		indent := strings.Repeat("  ", i+1)
		who := origin.User
		if who == "" {
			who = "unknown"
		}
		tprintRaw(fmt.Sprintf("%s%s %s/%s rev %d  %s  %s",
			indent, blameChainMarker(i), origin.SpaceSlug, origin.UnitSlug,
			origin.RevisionNum, origin.SetBy, who))
		if origin.Description != "" {
			tprintRaw(fmt.Sprintf("%s  %s%q%s", indent, colorDim, origin.Description, colorReset))
		}
	}
	if len(f.Chain) == 0 {
		tprintRaw("    (no recorded change; the value has been there since the unit was created)")
	}
	tprintRaw("")
}

// blameChainMarker distinguishes this unit's own record from the upstream hops the
// walk followed to reach the change that set the value.
func blameChainMarker(index int) string {
	if index == 0 {
		return "here:"
	}
	return "from:"
}

// blameOneLine collapses a multi-line value so it fits a table cell.
func blameOneLine(value string) string {
	value = strings.TrimSpace(value)
	if !strings.Contains(value, "\n") {
		return value
	}
	return strings.Join(strings.Fields(value), " ")
}

// blameAgo renders a timestamp the way a reader asks about it -- how long ago --
// rather than as a date they have to subtract.
func blameAgo(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

// anyGuarded reports whether any field carries a guard, so the legend explains a marker the
// reader can actually see rather than one this unit never uses.
func anyGuarded(fields []*blameField) bool {
	for _, f := range fields {
		if len(f.Guards) > 0 {
			return true
		}
	}
	return false
}
