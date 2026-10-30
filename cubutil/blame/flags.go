// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package blame

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/confighub/sdk/core/function/api"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

// A blame row has room for one line, and a field has more to say than fits on one: whether a
// merge leaves it alone, whether reasons are recorded for it, whether an upstream change to it
// was dropped, and how its value reached this unit. The FLAGS column says which of these there
// is to know, one letter each, and 'cub unit path' says what it is -- addressed by the row's
// REF, so the reader can copy one word rather than a resource and a path.

// Flag is one letter of the FLAGS column.
type Flag struct {
	Letter  string
	Meaning string
}

// FlagLegend is every flag, in the order the column prints them. P through W are about the
// field as it stands; U through N are about the change in this unit that put the value there,
// which for a value that arrived from upstream is the merge that brought it, not the upstream
// change the SET BY column names. The lowercase n and p say what part the field plays in
// NeedsProvides Links: they are about the field too, but nothing is wrong or pending because
// of either, so they come last and in lowercase.
var FlagLegend = []Flag{
	{"P", "protected: a local override a merge from upstream leaves alone"},
	{"G", "guarded: reasons are recorded for this value, and an operation must be cleared for them to overwrite it"},
	{"X", "conflict: an upstream change to this field was dropped by a merge or resolve"},
	{"A", "absent: the unit does not have this field; a conflict names a change that could not be made to it"},
	{"F", "finding: a Link's resolve could not do what it names here"},
	{"E", "validation error: a validating Trigger's failed check names this field, which blocks a release"},
	{"W", "validation warning: a validating Trigger with Warn set failed a check that names this field"},
	{"U", "an upgrade from upstream brought the value to this unit, through an UpgradeUnit Link"},
	{"L", "another Link set the value in this unit, by resolving or merging"},
	{"T", "a Trigger set the value in this unit"},
	{"C", "the change in this unit was made by a ChangeOrder"},
	{"N", "not released: the value changed after the unit's last Release, or the unit has none"},
	{"n", "needed: the value is to come from an upstream unit, through a NeedsProvides Link"},
	{"p", "provided: the value is offered to downstream units, through NeedsProvides Links"},
}

// blameFlags computes a field's FLAGS. lastReleasedRevisionNum is the Unit's: a value is
// released when the Revision that last wrote it is at or before the one the last Release
// bundled, whichever later Revision that Release was cut from. Reading the writing Revision's
// own Releases instead would call a value unreleased whenever an unrelated change followed it.
//
// Not released is the flag rather than released, since in a Space that releases nearly every
// value is released and the ones that are not are what a reader is looking for. A Space with
// no release Target has nothing to release, so releasable is false and nothing there is.
func blameFlags(f *Field, lastReleasedRevisionNum int64, releasable bool) string {
	var flags strings.Builder
	set := func(letter string, on bool) {
		if on {
			flags.WriteString(letter)
		}
	}
	set("P", f.Protected)
	set("G", len(f.Guards) > 0)
	set("X", len(f.Conflicts) > 0)
	set("A", f.Absent)
	set("F", len(f.Findings) > 0)
	set("E", hasValidation(f.Validation, false))
	set("W", hasValidation(f.Validation, true))
	var here *Origin
	if len(f.Chain) > 0 {
		here = f.Chain[0]
	}
	upgrade := here != nil && here.linkUpdateType == blameUpgradeLink
	set("U", upgrade)
	set("L", here != nil && here.Link != "" && !upgrade)
	set("T", here != nil && here.Trigger != "")
	set("C", here != nil && len(here.ChangeOrders) > 0)
	set("N", releasable && blameUnreleased(here, lastReleasedRevisionNum))
	set("n", len(f.Needed) > 0)
	set("p", len(f.Provided) > 0)
	return flags.String()
}

// hasValidation reports whether any of a field's failed checks is a warning, or an error.
func hasValidation(validations []Validation, warning bool) bool {
	for _, v := range validations {
		if v.Warning == warning {
			return true
		}
	}
	return false
}

// blameUpgradeLink is the Link UpdateType that upgrades a unit from the one it was cloned
// from. The U flag tells it from the other Links, which resolve values into the unit or
// merge another unit's changes, since an upgrade is how most of a variant arrives.
const blameUpgradeLink = "UpgradeUnit"

// blameUnreleased reports whether the change in this unit that put a value there came after
// the unit's last Release. A value with no recorded change has been there since the unit was
// created, and is unreleased only if the unit has never been released.
func blameUnreleased(here *Origin, lastReleasedRevisionNum int64) bool {
	if here == nil {
		return lastReleasedRevisionNum == 0
	}
	return here.RevisionNum > lastReleasedRevisionNum
}

// FlagsUsed returns the legend entries for the letters that appear in fields, so the
// legend explains what the reader can see rather than every letter there is.
func FlagsUsed(fields []*Field) []Flag {
	used := map[rune]bool{}
	for _, f := range fields {
		for _, letter := range f.Flags {
			used[letter] = true
		}
	}
	var legend []Flag
	for _, flag := range FlagLegend {
		if used[rune(flag.Letter[0])] {
			legend = append(legend, flag)
		}
	}
	return legend
}

// blamePathCovers reports whether a record about recordPath is about path: the same field, an
// ancestor whose whole value it was, or, when recordPath is empty, the resource as a whole.
func blamePathCovers(recordPath, path string) bool {
	recordPath = AuthoredPath(recordPath)
	return recordPath == "" || recordPath == path || strings.HasPrefix(path, recordPath+".")
}

// blameConflictsForPath returns the Unit's conflicts about one field.
func blameConflictsForPath(identifier api.ResourceIdentifier, conflicts *goclientnew.MutationConflictList,
	resourceType, resourceName, resourceNameCore, path string) []goclientnew.MutationConflict {
	if conflicts == nil {
		return nil
	}
	var matched []goclientnew.MutationConflict
	for _, conflict := range *conflicts {
		if conflict.Resource == nil || !blamePathCovers(conflict.Path, path) {
			continue
		}
		if _, found := matchBlameResourceInfo(identifier, []*goclientnew.ResourceInfo{conflict.Resource},
			resourceType, resourceName, resourceNameCore); found {
			matched = append(matched, conflict)
		}
	}
	return matched
}

// blameFindingsForPath returns the Unit's Link findings about one field. A ProvidedNotFound
// finding names a resource in the Link's upstream Unit, not this one, so it is about no field
// here however its path reads.
func blameFindingsForPath(identifier api.ResourceIdentifier, findings *goclientnew.LinkFindingList,
	resourceType, resourceName, resourceNameCore, path string) []goclientnew.LinkFinding {
	if findings == nil {
		return nil
	}
	var matched []goclientnew.LinkFinding
	for _, finding := range *findings {
		if finding.Resource == nil || finding.Reason == string(api.LinkFindingProvidedNotFound) ||
			!blamePathCovers(finding.Path, path) {
			continue
		}
		if _, found := matchBlameResourceInfo(identifier, []*goclientnew.ResourceInfo{finding.Resource},
			resourceType, resourceName, resourceNameCore); found {
			matched = append(matched, finding)
		}
	}
	return matched
}

// RefLength is how many hex digits a REF prints. A unit has hundreds of fields, not
// millions, so seven digits -- git's choice for the same job -- leave a collision unlikely, and
// 'cub unit path' takes any unambiguous prefix of at least RefMinLength.
const (
	RefLength    = 7
	RefMinLength = 4
)

// assignBlameRefs gives every field of one unit its REF.
//
// A REF is a hash of the resource type without its API version, the resource name without its
// scope, and the path, so the same field has the same REF in every Revision of the unit and in
// every variant of it: a REF copied from a base's blame finds the field in prod. Two resources
// of one type that differ only in scope -- the same name in two namespaces -- would share
// every REF that way, so those fields are hashed with the scoped name instead. Every field of
// the unit has to be in fields, filtered or not, for that test to see both resources.
func assignBlameRefs(identifier api.ResourceIdentifier, fields []*Field) {
	scopes := map[string]map[string]bool{}
	for _, f := range fields {
		key := blameRefResourceKey(identifier, f.ResourceType, f.resourceNameCore)
		if scopes[key] == nil {
			scopes[key] = map[string]bool{}
		}
		scopes[key][f.ResourceName] = true
	}
	for _, f := range fields {
		name := f.resourceNameCore
		if len(scopes[blameRefResourceKey(identifier, f.ResourceType, f.resourceNameCore)]) > 1 {
			name = f.ResourceName
		}
		f.Ref = blameRef(identifier, f.ResourceType, name, f.Path)
	}
}

func blameRefResourceKey(identifier api.ResourceIdentifier, resourceType, name string) string {
	return string(api.ResourceTypeWithoutAPIVersion(identifier, api.ResourceType(resourceType))) + "\x00" + name
}

// blameRef hashes one field's identity into its REF.
func blameRef(identifier api.ResourceIdentifier, resourceType, name, path string) string {
	sum := sha256.Sum256([]byte(blameRefResourceKey(identifier, resourceType, name) + "\x00" + path))
	return hex.EncodeToString(sum[:])[:RefLength]
}

// IsRef reports whether an argument is shaped like a REF or a prefix of one. A path can
// be too -- a top-level key "beef" -- which is why a REF that matches nothing is then tried as
// a path.
func IsRef(argument string) bool {
	if len(argument) < RefMinLength || len(argument) > RefLength {
		return false
	}
	for _, c := range argument {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
