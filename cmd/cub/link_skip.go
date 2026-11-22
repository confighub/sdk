// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"fmt"
	"slices"
	"strings"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/spf13/cobra"
)

const skipRevisionsHelp = `Each revision names one of the upstream unit's revisions, or several:

  7                          revision 7
  ChangeSet:<changeset>      the revisions of a changeset
  Tag:<tag>                  the revision the tag marks
  <from>..<to>               the revisions after <from>, up to and including <to>, as with git:
                             5..7 is revisions 6 and 7

Each end is a revision as --restore takes one: a number, HeadRevisionNum,
LastReleasedRevisionNum, Tag:<tag>, ChangeSet:<changeset>, Release:<number>, or Revision:<uuid>,
any of them prefixed with Before:. A bare slug is a tag.

Only MergeUnits and UpgradeUnit links skip revisions: the others take their upstream's head each
time. A link skips only revisions it has not merged; one skipped already takes the --reason and
--decline given. Skipping the revision just after the link's UpstreamLastMergedRevisionNum moves
it past every revision after that one that needs nothing merged.

A merge over the link -- an upgrade, a resolve, an automatic update -- passes over the revisions
it skipped, and takes the rest. A promotion of a change order takes its interval whole, skipped
revisions too, since it names them. "cub link get" lists the skips, and "cub link unskip" takes
them back.

--decline makes a skip final: the link still has not merged the revision, and no merge takes it,
not one naming it with --merge-revisions or --revisions, nor a change order whose range holds it.
Below the link's pointer it is not listed any more, so skips nobody will act on do not pile up.
--decline=false makes it an ordinary skip again, which naming it takes.`

const unskipRevisionsHelp = `Unskipping a revision after the link's UpstreamLastMergedRevisionNum makes it pending again: the
next merge takes it. Unskipping one at or below it lowers UpstreamLastMergedRevisionNum to just
under it, and lists what the link merged between there and where it was as merged above it
(UpstreamMergedAbove), so the next merge takes only what was unskipped.`

var (
	skipReason  string
	skipDecline bool
)

var linkSkipCmd = &cobra.Command{
	Use:   "skip <link> <revision>...",
	Short: "Skip upstream revisions, so merges over a link pass over them",
	Long: getCommandHelp(`Skip revisions of a link's upstream unit, so merges over the link pass over them.

`+skipRevisionsHelp+`

Examples:
`+"```"+`
  # Skip a changeset of the upstream, saying why
  cub link skip --space web-dev upgrade-backend ChangeSet:web-base/experiment --reason "not for dev"

  # Skip two revisions, and decline them: they will never be taken
  cub link skip --space web-dev upgrade-backend 7 9 --decline

  # List them again
  cub link skip --space web-dev upgrade-backend 7 9 --decline=false
`+"```"+`
`, ""),
	Args: cobra.MinimumNArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		return linkSkipCmdRun(cmd, args, true)
	},
}

var linkUnskipCmd = &cobra.Command{
	Use:   "unskip <link> <revision>...",
	Short: "Take back skips of upstream revisions, so merges over a link take them",
	Long: getCommandHelp(`Take back skips of revisions of a link's upstream unit, so the next merge over the link takes them.

The revisions are named as "cub link skip" names them.

`+unskipRevisionsHelp+`

Examples:
`+"```"+`
  cub link unskip --space web-dev upgrade-backend ChangeSet:web-base/experiment
`+"```"+`
`, ""),
	Args: cobra.MinimumNArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		return linkSkipCmdRun(cmd, args, false)
	},
}

func addSkipFlags(cmd *cobra.Command, skip bool) {
	if skip {
		cmd.Flags().StringVar(&skipReason, "reason", "", "why the revisions are skipped, shown with them by cub link get")
		cmd.Flags().BoolVar(&skipDecline, "decline", false, "make the skips final: no merge takes them, not even one naming them, and below the pointer they are not listed; --decline=false makes them ordinary skips again")
	}
	// A skip changes nothing about the link but what it skips, so it reads no entity and takes
	// none of the metadata flags an update does.
	enableDryRunFlag(cmd)
	addStandardDisplayFlags(cmd)
}

func init() {
	addSkipFlags(linkSkipCmd, true)
	addSkipFlags(linkUnskipCmd, false)
	linkCmd.AddCommand(linkSkipCmd)
	linkCmd.AddCommand(linkUnskipCmd)
}

// formatRevisionSelectors formats each entry for the server, which resolves it on each link's
// upstream unit: one revision specification, or a range <from>..<to>. Slugs are looked up in
// spaceID.
func formatRevisionSelectors(entries []string, spaceID string) ([]string, error) {
	var selectors []string
	for _, entry := range entries {
		from, to, isRange := strings.Cut(entry, "..")
		if !isRange {
			formatted, err := formatRangeEnd(entry, spaceID, 0)
			if err != nil {
				return nil, err
			}
			selectors = append(selectors, formatted)
			continue
		}
		if from == "" || to == "" {
			return nil, fmt.Errorf("a range is <from>..<to>, the revisions after <from> up to and including <to>; %q is missing one", entry)
		}
		formattedFrom, err := formatRangeEnd(from, spaceID, 0)
		if err != nil {
			return nil, err
		}
		formattedTo, err := formatRangeEnd(to, spaceID, 0)
		if err != nil {
			return nil, err
		}
		selectors = append(selectors, formattedFrom+".."+formattedTo)
	}
	return selectors, nil
}

// skipParams are the parameters of a skip, or an unskip, of selectors.
type skipParams struct {
	skipRevisions, unskipRevisions, skipReason *string
	declineSkips                               *bool
}

func newSkipParams(cmd *cobra.Command, selectors []string, skip bool) skipParams {
	joined := strings.Join(selectors, ",")
	if !skip {
		return skipParams{unskipRevisions: &joined}
	}
	params := skipParams{skipRevisions: &joined}
	if skipReason != "" {
		params.skipReason = &skipReason
	}
	if cmd.Flags().Changed("decline") {
		params.declineSkips = &skipDecline
	}
	return params
}

func linkSkipCmdRun(cmd *cobra.Command, args []string, skip bool) error {
	linkEnvelope, err := resolveLink(args[0], selectedSpaceID, "*")
	if err != nil {
		return err
	}
	link := linkEnvelope.Link
	// A bare slug names a tag of the upstream unit's space.
	selectors, err := formatRevisionSelectors(args[1:], link.ToSpaceID.String())
	if err != nil {
		return err
	}
	sp := newSkipParams(cmd, selectors, skip)
	params := &goclientnew.PatchLinkParams{
		SkipRevisions:   sp.skipRevisions,
		UnskipRevisions: sp.unskipRevisions,
		SkipReason:      sp.skipReason,
		DeclineSkips:    sp.declineSkips,
	}
	params.DryRun = dryRunParam()
	res, err := cubClientNew.PatchLinkWithBodyWithResponse(ctx, link.SpaceID, link.LinkID, params,
		"application/merge-patch+json", bytes.NewReader([]byte("{}")))
	if cubapi.IsAPIError(err, res) {
		return cubapi.InterpretErrorGeneric(err, res)
	}
	linkDetails := res.JSON200
	displayUpdateResults(linkDetails, "link", args[0], linkDetails.LinkID.String(), displayLinkDetails)
	if !verbose && !quiet && !isAlternativeOutput() {
		printLinkMarks(linkDetails)
	}
	return nil
}

// printLinkMarks prints what a link has skipped of its upstream's revisions, and what it merged
// above its pointer, as cub link get shows them.
func printLinkMarks(link *goclientnew.Link) {
	tprint("Upstream Last Merged Rev %d", link.UpstreamLastMergedRevisionNum)
	for _, run := range link.UpstreamSkipped {
		if reason := skippedRevisionsReason(run); reason != "" {
			tprint("%s: %s", skippedRevisionsLabel(run), reason)
		} else {
			tprint("%s", skippedRevisionsLabel(run))
		}
	}
	if len(link.UpstreamMergedAbove) > 0 {
		tprint("Upstream Revs Merged Above: %s", revisionNumRuns(link.UpstreamMergedAbove))
	}
}

var variantSkipCmd = &cobra.Command{
	Use:         "skip <variant> <revision>...",
	Short:       "Skip upstream revisions in every link into a variant",
	Annotations: map[string]string{"OrgLevel": ""},
	Long: getCommandHelp(`Skip revisions of the upstream units of a variant's links, so merges into the variant pass over them.

The links are the variant's MergeUnits and UpgradeUnit links from units of its space to units a
revision names: the units a changeset, tag or release marks -- the end of a range, or its start
when the end names none. Revision numbers differ from unit to unit, so they are not accepted here;
"cub link skip" takes them. --where narrows the links.

`+skipRevisionsHelp+`

Examples:
`+"```"+`
  # Leave a changeset of the base out of web/dev
  cub variant skip web/dev ChangeSet:web-base/experiment --reason "not for dev"

  # And decline it: dev will never take it
  cub variant skip web/dev ChangeSet:web-base/experiment --decline
`+"```"+`
`, ""),
	Args: cobra.MinimumNArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		return variantSkipCmdRun(cmd, args, true)
	},
}

var variantUnskipCmd = &cobra.Command{
	Use:         "unskip <variant> <revision>...",
	Short:       "Take back skips of upstream revisions in every link into a variant",
	Annotations: map[string]string{"OrgLevel": ""},
	Long: getCommandHelp(`Take back skips of revisions of the upstream units of a variant's links, so the next merges into the variant take them.

The links, and the revisions, are chosen as "cub variant skip" chooses them.

`+unskipRevisionsHelp+`

Examples:
`+"```"+`
  cub variant unskip web/dev ChangeSet:web-base/experiment
`+"```"+`
`, ""),
	Args: cobra.MinimumNArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		return variantSkipCmdRun(cmd, args, false)
	},
}

func init() {
	for _, cmd := range []*cobra.Command{variantSkipCmd, variantUnskipCmd} {
		addSkipFlags(cmd, cmd == variantSkipCmd)
		enableWhereFlag(cmd)
		variantCmd.AddCommand(cmd)
	}
}

// skipLinkChunk is how many upstream units one bulk skip names, to keep the where clause within
// the length of a query string.
const skipLinkChunk = 100

func variantSkipCmdRun(cmd *cobra.Command, args []string, skip bool) error {
	space, err := resolveVariantOrSpace(args[0], "SpaceID,Slug")
	if err != nil {
		return err
	}
	spaceID := space.Space.SpaceID.String()
	selectors, err := formatRevisionSelectors(args[1:], defaultSpaceID())
	if err != nil {
		return err
	}
	operation := "skip"
	if !skip {
		operation = "unskip"
	}

	// Each selector separately: the links to skip in are those whose upstream it names a
	// revision of, which differs from one selector to the next.
	var responses []goclientnew.LinkCreateOrUpdateResponse
	failed := false
	for i, selector := range selectors {
		from, to, isRange := strings.Cut(selector, "..")
		if !isRange {
			from, to = "Before:"+selector, selector
		}
		markWhere, err := revisionMarkWhere(to)
		if err != nil {
			return err
		}
		if markWhere == "" {
			if markWhere, err = revisionMarkWhere(from); err != nil {
				return err
			}
		}
		if markWhere == "" {
			return fmt.Errorf(`%s names revisions of one unit, and each unit numbers its own; name a changeset, tag or release, or use "cub link %s"`,
				args[1+i], operation)
		}
		upstreamIDs, err := unitsMarked(markWhere, "")
		if err != nil {
			return err
		}
		sp := newSkipParams(cmd, []string{selector}, skip)
		for chunk := range slices.Chunk(upstreamIDs, skipLinkChunk) {
			linkWhere := cubapi.NewWhere(fmt.Sprintf("SpaceID = '%s'", spaceID)).
				And("UpdateType IN ('UpgradeUnit', 'MergeUnits')").
				In("ToUnitID", chunk).
				And(where).String()
			include := "SpaceID,FromUnitID,ToUnitID,ToSpaceID"
			params := &goclientnew.BulkPatchLinksParams{
				Where:           &linkWhere,
				Include:         &include,
				SkipRevisions:   sp.skipRevisions,
				UnskipRevisions: sp.unskipRevisions,
				SkipReason:      sp.skipReason,
				DeclineSkips:    sp.declineSkips,
			}
			params.DryRun = dryRunParam()
			res, err := cubClientNew.BulkPatchLinksWithBodyWithResponse(ctx, params,
				"application/merge-patch+json", bytes.NewReader([]byte("{}")))
			if cubapi.IsAPIError(err, res) {
				return cubapi.InterpretErrorGeneric(err, res)
			}
			switch {
			case res.JSON200 != nil:
				responses = append(responses, *res.JSON200...)
			case res.JSON207 != nil:
				responses = append(responses, *res.JSON207...)
				failed = true
			}
		}
	}
	if len(responses) == 0 {
		if showProgress() {
			tprint("No links in %s to %s revisions in: none of its links are from an upstream unit the revisions are of", space.Space.Slug, operation)
		}
		return nil
	}
	status := 200
	if failed {
		status = 207
	}
	return handleBulkLinkUpdateResponse(&responses, &responses, status, operation, space.Space.Slug)
}
