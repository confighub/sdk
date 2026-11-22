// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"net/http"
	"slices"
	"sort"
	"strings"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/spf13/cobra"
)

var variantUnmergedArgs struct {
	skipped bool
}

var variantUnmergedCmd = &cobra.Command{
	Use:         "unmerged <variant>",
	Short:       "List the upstream revisions a variant has not merged",
	Annotations: map[string]string{"OrgLevel": ""},
	Long: getCommandHelp(`List the upstream revisions each unit of a variant has not merged, as git log --no-merged
does for a branch: what choosing what to promote next reads.

The revisions are those of the upstream unit of each of the variant's UpgradeUnit and MergeUnits
links that the link has neither merged nor skipped, oldest first, with the changeset and change
order each belongs to and its description. --skipped lists the skipped ones too, with why,
including those the link's pointer has moved past; a declined skip is listed while the link still
has earlier revisions to take. --where narrows the
links.

A revision listed is taken by the next plain upgrade of the unit, by "cub variant promote", or by
name with "cub variant promote --revisions"; "cub variant skip" leaves it out.

Examples:
`+"```"+`
  # What web/prod has still to take
  cub variant unmerged web/prod

  # And what it skipped, and why
  cub variant unmerged web/prod --skipped
`+"```"+`
`, ""),
	Args: cobra.ExactArgs(1),
	RunE: variantUnmergedCmdRun,
}

func init() {
	variantUnmergedCmd.Flags().BoolVar(&variantUnmergedArgs.skipped, "skipped", false, "list the skipped revisions too, with the reason each was skipped")
	enableWhereFlag(variantUnmergedCmd)
	addStandardDisplayFlags(variantUnmergedCmd)
	variantCmd.AddCommand(variantUnmergedCmd)
}

// unmergedRevision is one upstream revision a unit of the variant has not merged.
type unmergedRevision struct {
	Unit         string   `json:"Unit"`
	Link         string   `json:"Link"`
	RevisionNum  int64    `json:"RevisionNum"`
	ChangeSet    string   `json:"ChangeSet,omitempty"`
	ChangeOrders []string `json:"ChangeOrders,omitempty"`
	Description  string   `json:"Description,omitempty"`
	// Skipped is why the link skipped the revision, for a skipped one.
	Skipped  string `json:"Skipped,omitempty"`
	Declined bool   `json:"Declined,omitempty"`
}

func variantUnmergedCmdRun(cmd *cobra.Command, args []string) error {
	space, err := resolveVariantOrSpace(args[0], "SpaceID,Slug")
	if err != nil {
		return err
	}
	spaceID := space.Space.SpaceID
	links, err := cubapi.ListLinks(ctx, cubClient,
		cubapi.NewWhere(fmt.Sprintf("SpaceID = '%s'", spaceID)).
			And("UpdateType IN ('UpgradeUnit', 'MergeUnits')").
			And(where),
		cubapi.ListOpts{})
	if err != nil {
		return fmt.Errorf("failed to list the links of %s: %w", space.Space.Slug, err)
	}
	units, err := cubapi.ListUnits(ctx, cubClient, cubapi.NewWhere(fmt.Sprintf("SpaceID = '%s'", spaceID)),
		cubapi.ListOpts{Select: "UnitID,Slug,SpaceID", IncludeHidden: "*"})
	if err != nil {
		return fmt.Errorf("failed to list the units of %s: %w", space.Space.Slug, err)
	}
	unitSlugs := map[goclientnew.UUID]string{}
	for _, unit := range units {
		if unit.Unit != nil {
			unitSlugs[unit.Unit.UnitID] = unit.Unit.Slug
		}
	}

	var rows []unmergedRevision
	for _, extended := range links {
		link := extended.Link
		if link == nil {
			continue
		}
		skipped := map[int64]goclientnew.LinkSkippedRevisions{}
		// A skip can be below the pointer, which moved past it, and naming it still takes it, so
		// --skipped reads from the earliest skip.
		afterNum := link.UpstreamLastMergedRevisionNum
		for _, run := range link.UpstreamSkipped {
			for num := run.FromRevisionNum; num <= run.ToRevisionNum; num++ {
				skipped[num] = run
			}
			if variantUnmergedArgs.skipped {
				afterNum = min(afterNum, run.FromRevisionNum-1)
			}
		}
		revisions, err := upstreamRevisionsAfter(link.ToSpaceID, link.ToUnitID, afterNum)
		if err != nil {
			return err
		}
		for _, revision := range revisions {
			num := revision.Revision.RevisionNum
			if slices.Contains(link.UpstreamMergedAbove, num) {
				continue
			}
			if _, ok := skipped[num]; !ok && num <= link.UpstreamLastMergedRevisionNum {
				continue
			}
			row := unmergedRevision{
				Unit:        unitSlugs[link.FromUnitID],
				Link:        link.Slug,
				RevisionNum: num,
				Description: revision.Revision.Description,
			}
			if run, ok := skipped[num]; ok {
				if !variantUnmergedArgs.skipped {
					continue
				}
				row.Skipped, row.Declined = run.Reason, run.Declined
				if row.Skipped == "" {
					row.Skipped = "skipped"
				}
			}
			if revision.ChangeSet != nil {
				row.ChangeSet = revision.ChangeSet.Slug
			}
			for _, changeOrder := range revision.ChangeOrders {
				row.ChangeOrders = append(row.ChangeOrders, changeOrder.Slug)
			}
			sort.Strings(row.ChangeOrders)
			rows = append(rows, row)
		}
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Unit != rows[j].Unit {
			return rows[i].Unit < rows[j].Unit
		}
		return rows[i].RevisionNum < rows[j].RevisionNum
	})

	if renderPayload(rows) {
		return nil
	}
	if len(rows) == 0 {
		if !quiet {
			tprint("%s has merged every upstream revision its links follow", space.Space.Slug)
		}
		return nil
	}
	view := tableView()
	header := []string{"Unit", "Rev", "ChangeSet", "Change-Order", "Description"}
	if variantUnmergedArgs.skipped {
		header = append(header, "Skipped")
	}
	if !noheader {
		view.SetHeader(header)
	}
	for _, row := range rows {
		line := []string{row.Unit, fmt.Sprintf("%d", row.RevisionNum), row.ChangeSet, strings.Join(row.ChangeOrders, ","), row.Description}
		if variantUnmergedArgs.skipped {
			skippedText := row.Skipped
			if row.Declined {
				skippedText += " (declined)"
			}
			line = append(line, skippedText)
		}
		view.Append(line)
	}
	view.Render()
	return nil
}

// upstreamRevisionsAfter is the revisions of a unit after afterNum, with their changesets and
// change orders.
func upstreamRevisionsAfter(spaceID, unitID goclientnew.UUID, afterNum int64) ([]goclientnew.ExtendedRevision, error) {
	where := fmt.Sprintf("RevisionNum > %d", afterNum)
	include := "ChangeSetID,ChangeOrders"
	selectFields := "RevisionNum,RevisionID,UnitID,SpaceID,OrganizationID,Description,ChangeSetID,ChangeOrders"
	params := &goclientnew.ListExtendedRevisionsParams{Where: &where, Include: &include, Select: &selectFields}
	opts := cubapi.ListOpts{OrderBy: "ASC:RevisionNum"}
	params.OrderBy = &opts.OrderBy
	revisions, err := cubapi.ReadPages(opts, func(limit *int, token *string) (*http.Response, *[]goclientnew.ExtendedRevision, error) {
		params.Limit, params.Continue = limit, token
		res, err := cubClientNew.ListExtendedRevisionsWithResponse(ctx, spaceID, unitID, params)
		if cubapi.IsAPIError(err, res) {
			return nil, nil, cubapi.InterpretErrorGeneric(err, res)
		}
		return res.HTTPResponse, res.JSON200, nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list the revisions of unit %s: %w", unitID, err)
	}
	var out []goclientnew.ExtendedRevision
	for _, revision := range revisions {
		if revision != nil && revision.Revision != nil {
			out = append(out, *revision)
		}
	}
	return out, nil
}
