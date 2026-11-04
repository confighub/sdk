// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package blame

import "strings"

// Flag is one flag: the code the API returns, and the letter of the FLAGS column.
type Flag struct {
	Code    string
	Letter  string
	Meaning string
}

// FlagLegend is every flag, in the order the column prints them. P through W are about the
// field as it stands, or on a resource's own row the resource; U through N are about the change in this unit that put the value there,
// which for a value that arrived from upstream is the merge that brought it, not the upstream
// change the SET BY column names. The lowercase n and p say what part the field plays in
// NeedsProvides Links: they are about the field too, but nothing is wrong or pending because
// of either, so they come last and in lowercase.
var FlagLegend = []Flag{
	{"Protected", "P", "protected: a local override a merge from upstream leaves alone, by itself or as part of a resource protected as a whole"},
	{"Guarded", "G", "guarded: reasons are recorded for this value, and an operation must be cleared for them to overwrite it"},
	{"Conflict", "X", "conflict: an upstream change to this field was dropped by a merge or resolve"},
	{"Absent", "A", "absent: the unit does not have this field; a conflict names a change that could not be made to it"},
	{"Deleted", "D", "deleted: the unit does not have this field or resource because a change deleted it; with P, a merge from upstream leaves it deleted"},
	{"Finding", "F", "finding: a Link's resolve could not do what it names here"},
	{"ValidationError", "E", "validation error: a validating Trigger's failed check names this field, which blocks a release"},
	{"ValidationWarning", "W", "validation warning: a validating Trigger with Warn set failed a check that names this field"},
	{"Upgraded", "U", "an upgrade from upstream brought the value to this unit"},
	{"SetByLink", "L", "another Link set the value in this unit, by resolving or merging"},
	{"SetByTrigger", "T", "a Trigger set the value in this unit"},
	{"ChangeOrder", "C", "the change in this unit was made by a ChangeOrder"},
	{"Unreleased", "N", "not released: the value changed after the unit's last Release, or the unit has none"},
	{"Needed", "n", "needed: the value is to come from an upstream unit, through a NeedsProvides Link"},
	{"Provided", "p", "provided: the value is offered to downstream units, through NeedsProvides Links"},
}

// ResourcePathLabel is what the PATH column prints on the row about a resource as a whole,
// whose path is empty.
const ResourcePathLabel = "(resource)"

// PathLabel is a field's path as the PATH column prints it.
func PathLabel(f *Field) string {
	if f.Path == "" {
		return ResourcePathLabel
	}
	return f.Path
}

// Letters renders a field's flags as the FLAGS column prints them, one letter each, in
// FlagLegend order. A code this version does not know is left out.
func Letters(f *Field) string {
	var letters strings.Builder
	for _, flag := range FlagLegend {
		for _, code := range f.Flags {
			if code == flag.Code {
				letters.WriteString(flag.Letter)
			}
		}
	}
	return letters.String()
}

// FlagsUsed returns the legend entries for the letters that appear in fields, so the
// legend explains what the reader can see rather than every letter there is.
func FlagsUsed(fields []*Field) []Flag {
	used := map[string]bool{}
	for _, f := range fields {
		for _, code := range f.Flags {
			used[code] = true
		}
	}
	var legend []Flag
	for _, flag := range FlagLegend {
		if used[flag.Code] {
			legend = append(legend, flag)
		}
	}
	return legend
}
