// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"sort"
	"strings"
	"time"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/confighub/sdk/cubutil/blame"
	"github.com/google/uuid"
)

// unitPathLabelWidth aligns the values of a report's label: value lines.
const unitPathLabelWidth = 14

// displayUnitPath prints the report: the field as it stands, then each change from this unit
// out to where the value was set, then, with --history, every change this unit recorded.
func displayUnitPath(report *blame.PathReport) {
	f := report.Field
	names := report.Names
	unitPathLine(0, "Unit", f.SpaceSlug+"/"+f.UnitSlug)
	unitPathLine(0, "Resource", f.ResourceType+" "+f.ResourceName)
	if f.Path == "" {
		unitPathLine(0, "Path", "none: this is the row about the resource as a whole")
	} else {
		unitPathLine(0, "Path", wrapBlameText(f.Path, unitPathWrapWidth(0), ".,/", "-_"))
	}
	unitPathLine(0, "Ref", f.Ref)
	unitPathLine(0, "Flags", blame.Letters(f))
	switch {
	case f.Absent:
		unitPathLine(0, "Value", "none: the unit does not have this field")
	case f.Deleted && f.Path == "":
		unitPathLine(0, "Value", "none: a change deleted the resource from the unit")
	case f.Deleted:
		unitPathLine(0, "Value", "none: a change deleted the field from the unit")
	}
	unitPathValue(0, "Value", f.Value)
	unitPathLine(0, "Released", report.ReleaseMessage)
	switch {
	case f.Deleted && f.Protected:
		unitPathLine(0, "Protected", "yes: a merge from upstream leaves it deleted")
	case f.ResourceProtected && f.Path == "":
		unitPathLine(0, "Protected", "yes: the resource is protected as a whole, so a merge from upstream leaves all of it alone")
	case f.ResourceProtected:
		unitPathLine(0, "Protected", "yes: its resource is protected as a whole, so a merge from upstream leaves all of it alone")
	case f.Protected:
		unitPathLine(0, "Protected", "yes: a local override, which a merge from upstream leaves alone")
	}
	if len(f.Guards) > 0 {
		unitPathLine(0, "Guards", blame.FormatFieldGuards(f.Guards, f.InheritedGuards))
	}
	kinds := make([]string, 0, len(report.Annotations))
	for kind := range report.Annotations {
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)
	for _, kind := range kinds {
		unitPathLine(0, kind, blame.FormatGuards(report.Annotations[kind]))
	}
	for _, attribute := range f.Needed {
		unitPathLine(0, "Needed", blame.FormatNeeded(attribute))
	}
	for _, attribute := range f.Provided {
		unitPathLine(0, "Provided", blame.FormatProvided(attribute))
	}
	for _, dismissal := range report.ConflictDismissals {
		unitPathLine(0, "Dismissed", blame.FormatConflictDismissal(dismissal))
	}
	for _, conflict := range f.Conflicts {
		text := formatBlameConflict(conflict, 0)
		if upstream := pathReportName(names, conflict.UnitID); upstream != "" {
			text += "; from " + upstream
		}
		unitPathLine(0, "Conflict", text)
	}
	for _, validation := range f.Validation {
		unitPathLine(0, "Validation", blame.FormatValidation(validation))
	}
	for _, finding := range f.Findings {
		unitPathLine(0, "Finding", formatBlameFinding(finding))
	}

	tprintRaw("")
	switch {
	case f.Absent:
		tprintRaw("No recorded change: the unit does not have this field. Each conflict above is a change a")
		tprintRaw("merge could not make to it; 'cub unit conflicts' applies or dismisses them.")
	case len(report.Changes) == 0 && f.Path == "":
		tprintRaw("No recorded change: the resource has been there since the unit was created.")
	case len(report.Changes) == 0:
		tprintRaw("No recorded change: the value has been there since the unit was created.")
	case f.Deleted:
		tprintRaw("Changes, from this unit out to where it was deleted:")
		for _, change := range report.Changes {
			tprintRaw("")
			displayUnitPathChange(change, names)
		}
	case f.Path == "":
		tprintRaw("Changes, from this unit out to where the resource was added:")
		for _, change := range report.Changes {
			tprintRaw("")
			displayUnitPathChange(change, names)
		}
	default:
		tprintRaw("Changes, from this unit out to where the value was set:")
		for _, change := range report.Changes {
			tprintRaw("")
			displayUnitPathChange(change, names)
		}
	}

	if unitPathArgs.history {
		tprintRaw("")
		if len(report.History) == 0 {
			tprintRaw("History: this unit records no change to the field itself; it arrived with its resource.")
			return
		}
		if f.Path == "" {
			tprintRaw("History of the resource in this unit, oldest first (-o json for each change's records):")
		} else {
			tprintRaw("History of the field in this unit, oldest first (-o json for each change's records):")
		}
		table := tableView()
		if !noheader {
			table.SetHeader([]string{"Rev", "Mut", "Type", "Value", "Set By", "User", "When", "Change Order"})
		}
		for _, change := range report.History {
			table.Append([]string{
				fmt.Sprintf("%d", change.Origin.RevisionNum),
				fmt.Sprintf("%d", change.Origin.MutationNum),
				change.MutationType,
				truncateWithEllipsis(blameOneLine(change.Value), blameMaxValueWidth),
				change.Origin.SetBy,
				change.Origin.User,
				blameAgo(change.Origin.When),
				strings.Join(change.Origin.ChangeOrders, ","),
			})
		}
		table.Render()
	}
}

// displayUnitPathChange prints one change: the hop it is, what made it and why, and the
// Revision it is in, each line present only where the record has something to say.
func displayUnitPathChange(change blame.PathChange, names map[string]string) {
	origin := change.Origin
	tprintRaw(fmt.Sprintf("  %s/%s revision %d, mutation %d",
		origin.SpaceSlug, origin.UnitSlug, origin.RevisionNum, origin.MutationNum))
	unitPathLine(4, "Set by", origin.SetBy)
	if change.MutationType != "" {
		unitPathLine(4, "Mutation", change.MutationType)
	}
	if change.Value != "" {
		unitPathValue(4, "Value", change.Value)
	}

	if em := change.Mutation; em != nil && em.Mutation != nil {
		m := em.Mutation
		if link := em.Link; link != nil {
			line := link.Slug
			if link.UpdateType != "" {
				line += " (" + link.UpdateType + ")"
			}
			if upstream := pathReportName(names, link.ToUnitID); upstream != "" {
				line += " from " + upstream
			}
			unitPathLine(4, "Link", line)
			for _, upstreamPath := range link.UpstreamPaths {
				resource := ""
				if upstreamPath.Resource != nil {
					resource = upstreamPath.Resource.ResourceType + " " + upstreamPath.Resource.ResourceName + " "
				}
				unitPathLine(4, "", fmt.Sprintf("upstream %s = %s%s", upstreamPath.Name, resource, upstreamPath.Path))
			}
			for _, getter := range link.UpstreamGetters {
				unitPathLine(4, "", fmt.Sprintf("upstream %s = %s", getter.Name, formatFunctionInvocationLine(getter.FunctionInvocation)))
			}
		}
		if m.ProvidedPath != "" {
			provided := m.ProvidedPath
			if m.ProvidedResource != nil && m.ProvidedResource.ResourceName != "" {
				provided = m.ProvidedResource.ResourceType + " " + m.ProvidedResource.ResourceName + " " + provided
			}
			unitPathLine(4, "Provided by", provided)
		}
		if em.Trigger != nil {
			unitPathLine(4, "Trigger", em.Trigger.Slug)
		}
		if em.Invocation != nil {
			unitPathLine(4, "Invocation", em.Invocation.Slug)
		}
		if line := formatFunctionInvocationLine(m.FunctionInvocation); line != "" {
			unitPathLine(4, "Function", line)
		}
		if len(m.InvocationParams) != 0 {
			unitPathLine(4, "Parameters", jsonOneLine(m.InvocationParams))
		}
		if isSetUUID(m.MergeSourceID) {
			source := m.MergeSourceID.String()
			if em.MergeSource != nil {
				source = pathReportName(names, em.MergeSource.UnitID)
			}
			unitPathLine(4, "Merged from", fmt.Sprintf("%s, revisions %d to %d", source, m.MergeBaseRevisionNum, m.MergeEndRevisionNum))
		} else if origin.LinkUpstreamRevisionNum != 0 {
			// A Link's resolve that is not a merge reads the Link's upstream unit.
			unitPathLine(4, "Read from", fmt.Sprintf("%s, revision %d", origin.LinkUpstream, origin.LinkUpstreamRevisionNum))
		}
		if m.RestoredRevisionNum != 0 {
			unitPathLine(4, "Restored", fmt.Sprintf("revision %d", m.RestoredRevisionNum))
		}
		if m.ReplayOutcome != "" {
			unitPathLine(4, "Replay", strings.TrimSpace(m.ReplayOutcome+" "+m.ReplayReason))
		}
	}

	unitPathLine(4, "Operation", origin.Source)
	if origin.Description != "" {
		unitPathValue(4, "Description", origin.Description)
	}
	user := origin.User
	if extended := change.Revision; extended != nil && extended.Revision != nil && extended.Revision.UserAgent != "" {
		user += " using " + extended.Revision.UserAgent
	}
	unitPathLine(4, "User", user)
	if !origin.When.IsZero() {
		unitPathLine(4, "When", fmt.Sprintf("%s (%s)", origin.When.UTC().Format(time.DateTime+" MST"), blameAgo(origin.When)))
	}
	if extended := change.Revision; extended != nil && extended.ChangeSet != nil {
		unitPathLine(4, "ChangeSet", extended.ChangeSet.Slug)
	}
	if len(origin.ChangeOrders) > 0 {
		unitPathLine(4, "ChangeOrders", strings.Join(origin.ChangeOrders, ", "))
	}
	if extended := change.Revision; extended != nil {
		for _, release := range extended.Releases {
			unitPathLine(4, "Released in", formatUnitPathRelease(release, extended.ChangeOrders, origin, names))
		}
	}
	if len(origin.Tags) > 0 {
		unitPathLine(4, "Tags", strings.Join(origin.Tags, ", "))
	}
}

// formatUnitPathRelease describes a Release that bundled the Revision: its number, which is
// what 'cub release get' takes in the unit's Space, whether it was published and to which
// Target, and the ChangeOrder that cut it.
func formatUnitPathRelease(release goclientnew.Release, changeOrders []goclientnew.ChangeOrder, origin *blame.Origin,
	names map[string]string) string {
	text := fmt.Sprintf("release %d", release.ReleaseNum)
	if release.Published {
		text += ", published"
	} else {
		text += ", not published"
	}
	if isSetUUID(release.TargetID) {
		text += " to target " + pathReportName(names, *release.TargetID)
	}
	if isSetUUID(release.ChangeOrderID) {
		for _, changeOrder := range changeOrders {
			if changeOrder.ChangeOrderID != *release.ChangeOrderID {
				continue
			}
			// The hop names its ChangeOrders the way the CLI takes them, space-qualified when
			// they are in another Space; use that name rather than the bare slug.
			name := changeOrder.Slug
			for _, named := range origin.ChangeOrders {
				if named == changeOrder.Slug || strings.HasSuffix(named, "/"+changeOrder.Slug) {
					name = named
				}
			}
			text += ", by ChangeOrder " + name
		}
	}
	if !release.CreatedAt.IsZero() {
		text += ", " + blameAgo(release.CreatedAt)
	}
	return text
}

// unitPathWrapWidth is how wide a value may be after a label at the given indent before it
// wraps, or 0 when stdout is not a terminal and nothing wraps.
func unitPathWrapWidth(indent int) int {
	terminal := terminalWidth()
	if terminal <= 0 {
		return 0
	}
	return max(terminal-indent-unitPathLabelWidth-1, minWrapWidth)
}

// unitPathLine prints one label: value line, skipping an empty value. An empty label
// continues the line above it.
func unitPathLine(indent int, label, value string) {
	if value == "" {
		return
	}
	if label != "" {
		label += ":"
	}
	// A wrapped value continues under the first line's value, not under its label.
	pad := strings.Repeat(" ", indent)
	for i, line := range strings.Split(value, "\n") {
		tprintRaw(fmt.Sprintf("%s%-*s %s", pad, unitPathLabelWidth, label, line))
		if i == 0 {
			label = ""
		}
	}
}

// unitPathValue prints a value that may run to many lines, indented under its label.
func unitPathValue(indent int, label, value string) {
	value = strings.TrimRight(value, "\n")
	if !strings.Contains(value, "\n") {
		unitPathLine(indent, label, value)
		return
	}
	unitPathLine(indent, label, "|")
	pad := strings.Repeat(" ", indent+unitPathLabelWidth+1)
	for _, line := range strings.Split(value, "\n") {
		tprintRaw(pad + line)
	}
}

// pathReportName names a Unit or Target a path report refers to by ID, as the report's Names
// give it, or by the ID when they do not.
func pathReportName(names map[string]string, id uuid.UUID) string {
	if id == uuid.Nil {
		return ""
	}
	if name, ok := names[id.String()]; ok && name != "" {
		return name
	}
	return id.String()
}
