// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var variantDiffArgs struct {
	unifiedDiff bool
	colorOutput bool
}

var variantDiffCmd = &cobra.Command{
	Use:         "diff <space> <fromTag> [toTag]",
	Short:       "Show differences in a Variant between Tags",
	Annotations: map[string]string{"OrgLevel": ""},
	Long: getCommandHelp(`Show what changed across a whole variant space between two Tags.

A Tag marks one Revision in each Unit it was applied to, so naming two Tags compares the
space at two points in its history -- the state one release captured against the state
another did -- rather than one Unit at a time. Each Unit is diffed from its Revision on
the one side to its Revision on the other, and Units whose configuration is the same at
both print nothing.

Each side has to name something that picks out a Revision in every Unit of the space:

  <slug> | Tag:<slug>            the Revision that Tag marks
  ChangeSet:<slug>               where a Closed ChangeSet ended
  Before:ChangeSet:<slug>        the state it started from
  ChangeOrder:<slug>             where the change arrived
  Before:ChangeOrder:<slug>      the state before it
  HeadRevisionNum                each Unit's head Revision

A Unit with no Revision carrying the Tag is absent on that side rather than falling back
to its head, so it shows as a whole addition or removal and is labeled "absent". That is
where this differs from `+"`cub release publish --revision`"+`, which selects the same
Revision but has to bundle something for every Unit, and so falls back.

A revision number, a delta from head, a Revision ID and LastReleasedRevisionNum each pick
out a Revision of one Unit, and mean something different -- or nothing -- in the next one,
so they are refused here; `+"`cub unit diff`"+` is what takes them. Slugs resolve in
<space> unless qualified as space/slug.

<fromTag> is required. <toTag> defaults to HeadRevisionNum, so naming one Tag compares
that point against what the space holds now.

--where narrows the Units compared, taking the same expressions `+"`cub unit list`"+`
does.

Output Formats:
  - Default: Line-numbered format with color, under a header naming each Unit
  - Unified: Use -u for unified diff format (like git diff)
  - Color: Use -c to enable color in unified diff
  - Mutations: Use -o mutations for a structured mutation display

-o mutations shows what set each value rather than the lines that differ. A Unit's
MutationNums are one sequence, so the Mutations recorded at the to side that the from side
did not already carry are the ones the comparison is about, and those are what is listed;
a Unit the from side does not have lists everything, an addition having no prior state.
A Unit missing from the to side is named but lists nothing, since its removal is not
recorded as a Mutation of it.

Examples:
`+"```"+`
  # What the variant has changed since the release tagged v1.3.0
  cub variant diff apptique-prod v1.3.0

  # Between two releases
  cub variant diff apptique-prod v1.2.0 v1.3.0

  # What a change order brought to the variant
  cub variant diff apptique-prod Before:ChangeOrder:release-42 ChangeOrder:release-42

  # Only the workloads, as a unified diff
  cub variant diff -u apptique-prod v1.2.0 v1.3.0 --where "Slug LIKE 'deployment-%'"

  # What set each value that the change touched, rather than the lines
  cub variant diff apptique-prod v1.2.0 v1.3.0 -o mutations
`+"```"+`
`, ""),
	Args: cobra.RangeArgs(2, 3),
	RunE: variantDiffCmdRun,
}

func init() {
	variantDiffCmd.Flags().BoolVarP(&variantDiffArgs.unifiedDiff, "unified", "u", false, "output unified diff format")
	variantDiffCmd.Flags().BoolVarP(&variantDiffArgs.colorOutput, "color", "c", false, "colorize the unified diff output (default: true for numbered diff)")
	// Registered locally with a constrained description, as "cub unit diff" does: a diff is
	// text or mutations, not a structured entity payload, so json/yaml/jq/yq do not apply.
	variantDiffCmd.Flags().StringVarP(&outputFormat, "output", "o", "",
		`Output format: "`+variantDiffOutputDefault+`" for the text diff, or "`+variantDiffOutputMutations+`" for a resource-mutations diff.`)
	enableWhereFlag(variantDiffCmd)
	enableQuietFlag(variantDiffCmd)
	variantCmd.AddCommand(variantDiffCmd)
}

// The two forms a diff takes. An unset -o means variantDiffOutputDefault, which names the
// text diff so that it can be asked for as well as fallen into.
const (
	variantDiffOutputDefault   = "default"
	variantDiffOutputMutations = "mutations"
)

// variantDiffRevisionForms says what each side of the diff can name, for the error when it
// names something else.
const variantDiffRevisionForms = "name a Tag, a ChangeSet or a ChangeOrder -- optionally prefixed with Before: for the two that bound an interval -- or HeadRevisionNum"

// variantDiffHead is the one named revision a variant diff accepts, and the default for the
// to side. It needs no predicate of its own: a Unit's head is its highest-numbered Revision,
// which is the row the search endpoint keeps per Unit anyway.
const variantDiffHead = defaultTo

// parseVariantDiffRevision resolves one side of the diff to a selection that holds in every
// Unit of the space: HeadRevisionNum, or a Tag ID as "Tag:<uuid>".
//
// A whole space is compared at once, so each side has to pick out a Revision in every Unit,
// with one query rather than one per Unit. A Tag does; so do the boundaries of a ChangeSet
// and a ChangeOrder, which is how a whole change is compared rather than a Revision number
// that means something different in every Unit. So does HeadRevisionNum, a Unit's head being
// its highest-numbered Revision.
//
// LastReleasedRevisionNum does not, which is why it is refused where "cub unit diff" takes
// it. It is a Unit field recording the Revision the last Release of that Unit bundled, and
// there is no predicate over Revisions equal to it: the nearest, the highest-numbered
// Revision carrying a Release, is a different Revision as soon as a Release pins an earlier
// one to roll something back. A revision number, a delta from head and a Revision ID name a
// Revision of one Unit and are refused for the same reason -- which is also why the head
// revision handed to the parser below can be zero, deltas being the one form that needs it.
func parseVariantDiffRevision(revisionSpec string) (string, error) {
	// Checked before the parser, which refuses every named revision outright in resolveToTag
	// mode: they carry no entity type and reduce to no Tag.
	if revisionSpec == variantDiffHead {
		return revisionSpec, nil
	}

	identifier := strings.TrimPrefix(revisionSpec, "Before:")
	entityType := ""
	if parts := strings.SplitN(identifier, ":", 2); len(parts) == 2 {
		entityType, identifier = parts[0], parts[1]
	}
	perUnit := entityType != "Revision"
	if entityType == "" {
		// An unprefixed identifier is a Tag slug unless it names a Revision of one Unit:
		// a revision number, a bare Revision UUID, or a named revision. That is how
		// parseSelectedRevisionParameter reads it too.
		_, numErr := strconv.ParseInt(identifier, 10, 64)
		_, uuidErr := uuid.Parse(identifier)
		perUnit = numErr != nil && uuidErr != nil && !isNamedRevision(identifier)
	}
	if !perUnit {
		return "", errors.Newf("invalid revision '%s': %s, since a variant diff selects a Revision in every Unit of the space",
			revisionSpec, variantDiffRevisionForms)
	}

	// resolveToTag rather than serverResolvedRevision: the selection is made here, by one
	// query per side, so a ChangeSet or ChangeOrder has to come down to the Tag bounding it
	// rather than being left for the server to resolve per Unit.
	formatted, _, err := parseSelectedRevisionParameter(revisionSpec, resolveToTag, 0)
	if err != nil {
		return "", err
	}
	return formatted, nil
}

func variantDiffCmdRun(cmd *cobra.Command, args []string) error {
	switch outputFormat {
	case "", variantDiffOutputDefault, variantDiffOutputMutations:
	default:
		return errors.Newf(`"cub variant diff" accepts -o %q or -o %q; %q is not supported`,
			variantDiffOutputDefault, variantDiffOutputMutations, outputFormat)
	}

	// Args bounds the count, so the from side is always present and the to side is the
	// only one with a default.
	fromTag := args[1]
	toTag := variantDiffHead
	if len(args) == 3 {
		toTag = args[2]
	}

	space, err := apiGetSpaceFromSlug(args[0], "SpaceID,Slug")
	if err != nil {
		return err
	}
	// As "variant approve" and "variant promote" do: this command names its space
	// positionally, so the selected space may be unset or "*". Point it at the space being
	// compared, so that a bare Tag slug on either side resolves there.
	selectedSpaceID = space.SpaceID.String()
	selectedSpaceSlug = space.Slug

	fromRevision, err := parseVariantDiffRevision(fromTag)
	if err != nil {
		return errors.Wrap(err, "invalid source tag")
	}
	toRevision, err := parseVariantDiffRevision(toTag)
	if err != nil {
		return errors.Wrap(err, "invalid target tag")
	}

	return variantDiff(space, where,
		variantDiffSide{name: fromTag, revision: fromRevision},
		variantDiffSide{name: toTag, revision: toRevision})
}

// variantDiffSide is one side of the comparison: what the caller named it, and what that
// resolved to. Both are kept because the resolved form is a Tag ID, which is not what anyone
// asked for and not what the report should say.
type variantDiffSide struct {
	name     string
	revision string
}

// variantDiffRevisionWhere selects, for one side of the diff, the Revision of every Unit in
// the space.
//
// HeadRevisionNum needs no predicate of its own: the search endpoint keeps one row per
// Unit, the highest-numbered, and a Unit's head is its highest-numbered Revision. A Tag
// carries its own predicate and marks one Revision per Unit, so that dedup is left with
// nothing to choose between.
//
// Both terms have to be ones the database evaluates. A term reaching through the Revision's
// UnitID reference, "RevisionNum = Unit.HeadRevisionNum", is evaluated in memory over the
// rows the query returned, and those rows have already been reduced to one per Unit -- so it
// silently drops every Unit whose head is not the Revision the term names, rather than
// selecting a different Revision.
func variantDiffRevisionWhere(spaceID uuid.UUID, revisionSpec string) string {
	if revisionSpec == variantDiffHead {
		return fmt.Sprintf("SpaceID = '%s'", spaceID)
	}
	return fmt.Sprintf("SpaceID = '%s' AND Tags ? '%s'", spaceID, strings.TrimPrefix(revisionSpec, "Tag:"))
}

// fetchVariantDiffRevisions reads one side of the diff: the configuration of the selected
// Revision of every Unit in the space, keyed by Unit. One request covers the whole space
// however many Units it holds, which is what the bulk data endpoint exists for -- reading
// each side of each Unit separately would be two requests per Unit.
func fetchVariantDiffRevisions(spaceID uuid.UUID, revisionSpec string) (map[uuid.UUID]*goclientnew.RevisionData, error) {
	whereClause := variantDiffRevisionWhere(spaceID, revisionSpec)
	params := &goclientnew.SearchRevisionDataParams{Where: &whereClause}
	res, err := cubClientNew.SearchRevisionDataWithResponse(ctx, params)
	if cubapi.IsAPIError(err, res) {
		return nil, cubapi.InterpretErrorGeneric(err, res)
	}
	byUnit := make(map[uuid.UUID]*goclientnew.RevisionData)
	if res.JSON200 != nil {
		for _, revisionData := range *res.JSON200 {
			byUnit[revisionData.UnitID] = &revisionData
		}
	}
	return byUnit, nil
}

// variantMutationsDiff prints, for each selected Unit, the Mutations between the two sides. It
// is the -o mutations counterpart of variantTextDiff, and reports the same three outcomes --
// changed, unchanged, at neither, over the same Revisions.
//
// Each Unit's two sides are handed to displayMutationsFromDryRun, so a Unit here is displayed
// the way "cub unit diff -o mutations" displays it.
func variantMutationsDiff(space *goclientnew.Space, units []*goclientnew.Unit, from, to variantDiffSide) error {
	fromRevisions, err := fetchVariantDiffRevisions(space.SpaceID, from.revision)
	if err != nil {
		return errors.Wrapf(err, "failed to read %s", from.name)
	}
	toRevisions, err := fetchVariantDiffRevisions(space.SpaceID, to.revision)
	if err != nil {
		return errors.Wrapf(err, "failed to read %s", to.name)
	}

	changed, unchanged, absent := 0, 0, 0
	for _, unit := range units {
		fromData, toData := fromRevisions[unit.UnitID], toRevisions[unit.UnitID]
		if fromData == nil && toData == nil {
			absent++
			continue
		}
		if variantDiffUnchanged(fromData, toData) {
			unchanged++
			continue
		}
		changed++

		fromLabel := variantDiffSideLabel(space.Slug, unit.Slug, fromData)
		toLabel := variantDiffSideLabel(space.Slug, unit.Slug, toData)
		fmt.Printf("%s=== %s -> %s%s\n", colorDim, fromLabel, toLabel, colorReset)

		// The Mutations displayed are of this Unit, so their details and old values resolve
		// against it.
		lookupMutationsUnitID = unit.UnitID.String()
		// A Unit the to side does not have contributes no Revision to compare against, which
		// leaves the changed side empty rather than falling back to the Unit's head.
		changed := changedRevision{UnitID: unit.UnitID, Data: variantDiffData(toData)}
		if toData != nil {
			changed.RevisionID = toData.RevisionID
		}
		displayMutationsFromDryRun(variantDiffData(fromData), changed, space.SpaceID.String(), "diff")
	}

	if !quiet {
		tprint("%d of %d unit(s) changed between %s and %s in %s (%d unchanged, %d at neither)",
			changed, len(units), from.name, to.name, space.Slug, unchanged, absent)
	}
	return nil
}

// variantDiffSideLabel names one side of a Unit's diff. A Unit with no Revision on a side --
// it did not exist yet, or nothing in it carries that Tag -- is labeled rather than omitted,
// since the whole content that follows is an addition or a removal rather than an edit.
func variantDiffSideLabel(spaceSlug, unitSlug string, revisionData *goclientnew.RevisionData) string {
	if revisionData == nil {
		return fmt.Sprintf("%s/%s/absent", spaceSlug, unitSlug)
	}
	return formatDiffLabel(spaceSlug, unitSlug, revisionData.RevisionNum)
}

// variantDiffData is the configuration to compare for one side, empty where the Unit has no
// Revision there.
func variantDiffData(revisionData *goclientnew.RevisionData) string {
	if revisionData == nil {
		return ""
	}
	return revisionData.Data
}

// variantDiffUnchanged reports whether a Unit's two sides hold the same configuration. The
// hashes answer without comparing the bodies, and are equal only if both sides exist.
func variantDiffUnchanged(from, to *goclientnew.RevisionData) bool {
	if from == nil || to == nil {
		return from == nil && to == nil
	}
	if from.DataHash != "" && to.DataHash != "" {
		return from.DataHash == to.DataHash
	}
	return from.Data == to.Data
}

// variantDiff selects the Units to compare and hands them to whichever diff -o asked for.
//
// The Units are listed rather than the Revisions grouped, because the selection is over Units
// (--where names Unit attributes) and because a Revision carries no Unit slug. Sorting by slug
// is what makes the order of the report the same from one run to the next.
func variantDiff(space *goclientnew.Space, selectionWhere string, from, to variantDiffSide) error {
	units, err := apiListUnits(space.SpaceID.String(), selectionWhere, "UnitID,Slug")
	if err != nil {
		return err
	}
	if len(units) == 0 {
		return errors.Newf("no units selected in space %s", space.Slug)
	}
	slices.SortFunc(units, func(a, b *goclientnew.Unit) int {
		return strings.Compare(a.Slug, b.Slug)
	})

	if outputFormat == variantDiffOutputMutations {
		return variantMutationsDiff(space, units, from, to)
	}
	return variantTextDiff(space, units, from, to)
}

// variantTextDiff prints, for each selected Unit, the difference between its configuration on
// the two sides, honoring --unified and --color.
//
// The two sides are read whole, one request each, and then paired up per Unit. Units whose
// configuration is the same on both sides print nothing: a variant is a whole space, so a diff
// that reprinted every unchanged Unit would bury the ones that moved.
func variantTextDiff(space *goclientnew.Space, units []*goclientnew.Unit, from, to variantDiffSide) error {
	fromRevisions, err := fetchVariantDiffRevisions(space.SpaceID, from.revision)
	if err != nil {
		return errors.Wrapf(err, "failed to read %s", from.name)
	}
	toRevisions, err := fetchVariantDiffRevisions(space.SpaceID, to.revision)
	if err != nil {
		return errors.Wrapf(err, "failed to read %s", to.name)
	}

	changed, unchanged, absent := 0, 0, 0
	for _, unit := range units {
		fromData, toData := fromRevisions[unit.UnitID], toRevisions[unit.UnitID]
		if fromData == nil && toData == nil {
			// The Unit is at neither point in the space's history, so it is no part of this
			// comparison -- it is not an addition or a removal between the two.
			absent++
			continue
		}
		if variantDiffUnchanged(fromData, toData) {
			unchanged++
			continue
		}
		changed++

		fromLabel := variantDiffSideLabel(space.Slug, unit.Slug, fromData)
		toLabel := variantDiffSideLabel(space.Slug, unit.Slug, toData)
		segments := ComputeStructuredDiff(variantDiffData(fromData), variantDiffData(toData))
		if variantDiffArgs.unifiedDiff {
			// The unified format names both sides in its own header, so the Unit is already
			// identified without one of ours.
			printUnifiedDiff(segments, fromLabel, toLabel, variantDiffArgs.colorOutput)
			continue
		}
		// The numbered format carries no file names, so without a header a diff of many Units
		// is one run of line numbers with nothing saying where each Unit starts.
		fmt.Printf("%s=== %s -> %s%s\n", colorDim, fromLabel, toLabel, colorReset)
		printNumberedDiff(segments)
	}

	if !quiet {
		tprint("%d of %d unit(s) changed between %s and %s in %s (%d unchanged, %d at neither)",
			changed, len(units), from.name, to.name, space.Slug, unchanged, absent)
	}
	return nil
}
