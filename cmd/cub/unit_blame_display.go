// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"strings"
	"time"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/confighub/sdk/cubutil/blame"
	"github.com/olekukonko/tablewriter"
)

// blameMaxValueWidth keeps a long value -- a probe body, a base64 blob -- from
// pushing the columns that answer the question off the screen. --verbose prints the
// value whole underneath instead, and 'cub unit path' prints it whole.
const blameMaxValueWidth = 40

// displayBlame renders the field table, grouped by resource, and by unit first when byUnit
// is set, for a blame of many units. One line per field: its REF and FLAGS, and the change
// that put the value there after following upstream, with who made it. The value itself
// is in -o wide, beside the function, the mutation, and how the value reached this unit.
// With --verbose each field also gets its change description and the hops in between,
// which is the same information the columns summarize.
func displayBlame(fields []*blame.Field, byUnit bool) {
	if len(fields) == 0 {
		tprintRaw("No fields")
		return
	}
	wide := effectiveOutput().Kind == OutputWide
	header := blameHeader(wide)

	// Every row is built first so each resource's table can be fitted to the terminal before
	// it is printed. Each resource is a table of its own, sized to its own rows, so each is
	// fitted to its own; --verbose's one-row tables take their resource's fit, so its rows
	// still line up.
	rows := make([][]string, len(fields))
	for i, f := range fields {
		rows[i] = blameRow(f, wide)
	}
	terminal := terminalWidth()
	for start := 0; start < len(fields); {
		end := start + 1
		for end < len(fields) && blameTableKey(fields[end]) == blameTableKey(fields[start]) {
			end++
		}
		fitBlameTable(rows[start:end], header, wide, terminal)
		start = end
	}

	unit, resource := "", ""
	var table *tablewriter.Table
	flush := func() {
		if table != nil {
			table.Render()
			table = nil
		}
	}

	for i, f := range fields {
		if key := f.SpaceSlug + "/" + f.UnitSlug; byUnit && key != unit {
			flush()
			unit, resource = key, ""
			tprintRaw("")
			tprintRaw("Unit: " + key)
		}
		newResource := false
		if key := f.ResourceType + " " + f.ResourceName; key != resource {
			flush()
			resource, newResource = key, true
			tprintRaw("")
			tprintRaw(fmt.Sprintf("%sResource: %s%s", colorLightBlue, resource, colorReset))
		}
		// --verbose prints each field's detail under its row, so each row is a table of its
		// own and only the resource's first carries the header.
		if table == nil {
			table = tableView()
			if !noheader && newResource {
				table.SetHeader(header)
			}
		}
		table.Append(rows[i])

		if verbose {
			flush()
			displayBlameDetail(f)
		}
	}
	flush()

	if !noheader {
		if legend := blame.FlagsUsed(fields); len(legend) > 0 {
			tprintRaw("")
			for _, flag := range legend {
				tprintRaw(fmt.Sprintf("%s%s  %s%s", colorDim, flag.Letter, flag.Meaning, colorReset))
			}
		}
		tprintRaw(fmt.Sprintf("%sFor everything recorded about a field: cub unit path <unit> <ref>%s", colorDim, colorReset))
	}
}

// blameTableKey is what a field's table is: one per resource of each unit.
func blameTableKey(f *blame.Field) string {
	return f.SpaceSlug + "/" + f.UnitSlug + "\x00" + f.ResourceType + " " + f.ResourceName
}

// blameShrink is a column a table too wide for the terminal may wrap, how narrow it may get,
// and where its text may break.
type blameShrink struct {
	column    int
	minWidth  int
	breakSets []string
}

// blameShrinkOrder is which columns give up width when a table is wider than the terminal,
// first to last. PATH goes first: it is the widest, and REF is the handle a reader copies,
// so a path read across lines loses nothing. A path breaks after the dots between its
// segments, the commas between the keys of an associative one, and the slashes in a key,
// and inside a name only for a segment wider than the column. SET BY breaks between its
// words first; the names in the other columns break after the punctuation inside them.
var blameShrinkOrder = []blameShrink{
	// REF FLAGS PATH SET-BY USER WHERE REV WHEN
	{2, 30, []string{".,/", "-_"}},
	{3, 14, []string{" ", "-@/._"}},
	{5, 12, []string{"-@/._"}},
	{4, 12, []string{"-@/._"}},
}

// fitBlameTable wraps the cells of one table so that it fits a terminal of the given width,
// taking width from the columns in blameShrinkOrder until it fits or each is at its minimum.
// A width of 0 means the output is not a terminal, and nothing wraps; -o wide does not
// either, since it is asked for to read every column whole.
//
// The table is as wide as its columns, each as wide as its widest line plus the padding after
// it -- the last column's too, since it is printed as trailing spaces. One column is left
// spare: a line exactly as wide as the terminal wraps on some of them, and leaves a blank one.
func fitBlameTable(rows [][]string, header []string, wide bool, terminal int) {
	if terminal <= 0 || wide {
		return
	}
	widths := make([]int, len(header))
	total := 0
	for column := range header {
		widths[column] = len(header[column])
		for _, row := range rows {
			widths[column] = max(widths[column], len(row[column]))
		}
		total += widths[column] + len(tablePadding)
	}
	for _, shrink := range blameShrinkOrder {
		excess := total - (terminal - 1)
		if excess <= 0 {
			break
		}
		narrowed := max(widths[shrink.column]-excess, shrink.minWidth, len(header[shrink.column]))
		if narrowed >= widths[shrink.column] {
			continue
		}
		total -= widths[shrink.column] - narrowed
		widths[shrink.column] = narrowed
		for _, row := range rows {
			row[shrink.column] = wrapBlameText(row[shrink.column], narrowed, shrink.breakSets...)
		}
	}
}

// wrapBlameText breaks text onto lines no wider than width, after any of the characters in
// the first of breakSets, so that a path or a name reads down its cell. A piece still too wide
// is broken after the characters of the next set, and so on -- a path breaks between its
// segments where it can, and inside a name only when a segment is wider than the column. A
// piece with nowhere left to break is split where the width runs out, which is what keeps the
// table inside the terminal.
func wrapBlameText(text string, width int, breakSets ...string) string {
	if width <= 0 || len(text) <= width {
		return text
	}
	var lines []string
	line := ""
	for _, piece := range splitBlameText(text, width, breakSets) {
		if line != "" && len(line)+len(piece) > width {
			lines = append(lines, line)
			line = ""
		}
		line += piece
	}
	if line != "" {
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

// splitBlameText cuts text into the pieces wrapBlameText packs onto lines: after each
// character of the first break set, then each piece still wider than width after those of
// the next, and finally at width.
func splitBlameText(text string, width int, breakSets []string) []string {
	if len(text) <= width {
		return []string{text}
	}
	if len(breakSets) == 0 {
		var pieces []string
		for len(text) > width {
			pieces = append(pieces, text[:width])
			text = text[width:]
		}
		return append(pieces, text)
	}
	var pieces []string
	piece := ""
	for _, c := range text {
		piece += string(c)
		if strings.ContainsRune(breakSets[0], c) {
			pieces = append(pieces, splitBlameText(piece, width, breakSets[1:])...)
			piece = ""
		}
	}
	if piece != "" {
		pieces = append(pieces, splitBlameText(piece, width, breakSets[1:])...)
	}
	return pieces
}

func blameHeader(wide bool) []string {
	if wide {
		return []string{"Ref", "Flags", "Path", "Value", "Set By", "Function", "User", "Where", "Rev", "Mut", "When", "Via", "Change Order"}
	}
	return []string{"Ref", "Flags", "Path", "Set By", "User", "Where", "Rev", "When"}
}

// blameRow is one field's line. SET BY, USER, WHERE, REV and WHEN describe the change that
// set the value, after following upstream. VIA and CHANGE ORDER describe the change in this
// unit that brought it, which is the same change unless the value arrived from upstream.
func blameRow(f *blame.Field, wide bool) []string {
	origin := f.Origin()
	setBy, function, user, where, rev, mut, when := "", "", "", "", "", "", ""
	if origin != nil {
		setBy = origin.SetBy
		// The function is named on its own only where SET BY named something else, such as
		// the Link that ran it.
		if origin.Function != setBy {
			function = origin.Function
		}
		user = origin.User
		where = origin.SpaceSlug
		if origin.RevisionNum != 0 {
			rev = fmt.Sprintf("%d", origin.RevisionNum)
		}
		if origin.MutationNum != 0 {
			mut = fmt.Sprintf("%d", origin.MutationNum)
		}
		when = blameAgo(origin.When)
	}
	if !wide {
		return []string{f.Ref, f.Flags, f.Path, setBy, user, where, rev, when}
	}
	via, changeOrders := "", ""
	if len(f.Chain) > 0 {
		here := f.Chain[0]
		if len(f.Chain) > 1 {
			via = here.SetBy
		}
		changeOrders = strings.Join(here.ChangeOrders, ",")
	}
	return []string{
		f.Ref, f.Flags, f.Path,
		truncateWithEllipsis(blameOneLine(f.Value), blameMaxValueWidth),
		setBy, function, user, where, rev, mut, when, via, changeOrders,
	}
}

// displayBlameDetail prints one field's full record: the value as it stands, and
// every hop from this unit out to where the value was set, each with who made the
// change and what they called it.
func displayBlameDetail(f *blame.Field) {
	// The reasons first: they are what an operation about to write here has to know, and
	// unlike the chain they are about the value now rather than about how it got here.
	if len(f.Guards) > 0 {
		tprintRaw(fmt.Sprintf("    %sguarded: %s%s", colorDim, blame.FormatGuards(f.Guards), colorReset))
	}
	for _, conflict := range f.Conflicts {
		tprintRaw(fmt.Sprintf("    %sconflict: %s%s", colorDim, formatBlameConflict(conflict, blameMaxValueWidth), colorReset))
	}
	for _, validation := range f.Validation {
		tprintRaw(fmt.Sprintf("    %svalidation %s%s", colorDim, blame.FormatValidation(validation), colorReset))
	}
	for _, finding := range f.Findings {
		tprintRaw(fmt.Sprintf("    %sfinding: %s%s", colorDim, formatBlameFinding(finding), colorReset))
	}
	for _, attribute := range f.Needed {
		tprintRaw(fmt.Sprintf("    %sneeded: %s%s", colorDim, blame.FormatNeeded(attribute), colorReset))
	}
	for _, attribute := range f.Provided {
		tprintRaw(fmt.Sprintf("    %sprovided: %s%s", colorDim, blame.FormatProvided(attribute), colorReset))
	}
	for i, origin := range f.Chain {
		indent := strings.Repeat("  ", i+1)
		who := origin.User
		if who == "" {
			who = "unknown"
		}
		setBy := origin.SetBy
		if origin.Function != "" && origin.Function != setBy {
			setBy += " (" + origin.Function + ")"
		}
		line := fmt.Sprintf("%s%s %s/%s rev %d  %s  %s",
			indent, blameChainMarker(i), origin.SpaceSlug, origin.UnitSlug,
			origin.RevisionNum, setBy, who)
		if len(origin.ChangeOrders) > 0 {
			line += "  changeorder " + strings.Join(origin.ChangeOrders, ",")
		}
		tprintRaw(line)
		if origin.Description != "" {
			tprintRaw(fmt.Sprintf("%s  %s%q%s", indent, colorDim, origin.Description, colorReset))
		}
	}
	switch {
	case f.Absent:
		tprintRaw("    (not in the unit: the conflicts above are changes a merge could not make here)")
	case len(f.Chain) == 0:
		tprintRaw("    (no recorded change; the value has been there since the unit was created)")
	}
	tprintRaw("")
}

// formatBlameConflict renders a conflict on one line: why the upstream change was dropped,
// and what it would have written, cut to maxValueWidth when that is not 0.
func formatBlameConflict(conflict goclientnew.MutationConflict, maxValueWidth int) string {
	text := conflict.Reason
	if conflict.Path != "" {
		text += " at " + blame.AuthoredPath(conflict.Path)
	} else {
		text += " on the whole resource"
	}
	if conflict.Source != nil && conflict.Source.Value != "" {
		value := blameOneLine(conflict.Source.Value)
		if maxValueWidth > 0 {
			value = truncateWithEllipsis(value, maxValueWidth)
		}
		text += fmt.Sprintf("; upstream wrote %q", value)
	}
	if conflict.Guard != nil {
		text += fmt.Sprintf("; guard %s=%s", conflict.Guard.Key, conflict.Guard.Value)
	}
	if conflict.Details != "" {
		text += "; " + conflict.Details
	}
	return text
}

// formatBlameFinding renders a Link finding on one line.
func formatBlameFinding(finding goclientnew.LinkFinding) string {
	text := finding.Reason
	if finding.Path != "" {
		text += " at " + finding.Path
	}
	if finding.Entry != "" {
		text += " for " + finding.Entry
	}
	if finding.Details != "" {
		text += "; " + finding.Details
	}
	return text
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
