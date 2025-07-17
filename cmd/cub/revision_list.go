// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"net/url"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
)

var revisionListCmd = &cobra.Command{
	Use:   "list <unit>",
	Short: "List revisions",
	Long: `List revisions for a unit in a space. Revisions track the history of changes made to a unit's configuration.

Examples:
  # List all revisions for a unit
  cub revision list --space my-space my-ns

  # List revisions without headers
  cub revision list --space my-space --no-header my-ns

  # List revisions in JSON format
  cub revision list --space my-space --json my-ns

  # List revisions using unit ID instead of slug
  cub revision list --space my-space --by-unit-id 61f26b06-3c34-4363-8b9d-7d0a7c2b5f1c

  # List revisions with custom JQ filter
  cub revision list --space my-space --jq '.[].RevisionNum' my-ns

  # List revisions with specific criteria
  cub revision list --space my-space --where 'RevisionNum > 1' my-ns

  # List revisions with extended information
  cub revision list --space my-space --json --extended my-ns`,
	Args: cobra.ExactArgs(1),
	RunE: revisionListCmdRun,
}

var byUnitID bool

func init() {
	addStandardListFlags(revisionListCmd)
	revisionListCmd.Flags().BoolVar(&byUnitID, "by-unit-id", false, "use unit id instead of slug")
	revisionCmd.AddCommand(revisionListCmd)
}

func revisionListCmdRun(cmd *cobra.Command, args []string) error {
	var unit *goclientnew.Unit
	var err error
	if byUnitID {
		unit, err = apiGetUnit(args[0])
	} else {
		unit, err = apiGetUnitFromSlug(args[0])
	}
	if err != nil {
		return err
	}
	revisions, err := apiListRevisions(selectedSpaceID, unit.UnitID.String(), where)
	if err != nil {
		return err
	}
	displayListResults(revisions, getRevisionSlug, displayRevisionList)
	return nil
}

func getRevisionSlug(revision *goclientnew.Revision) string {
	// Use number
	return fmt.Sprintf("%d", revision.RevisionNum)
}

func displayRevisionList(revisions []*goclientnew.Revision) {
	table := tableView()
	if !noheader {
		table.SetHeader([]string{"ID", "Num", "Time", "User-ID", "Source", "Description", "Apply-Gates"})
	}
	for _, rev := range revisions {
		applyGates := "None"
		if rev.ApplyGates != nil {
			if len(rev.ApplyGates) > 1 {
				applyGates = "Multiple"
			} else {
				for key := range rev.ApplyGates {
					applyGates = key
				}
			}
		}
		table.Append([]string{
			rev.RevisionID.String(),
			fmt.Sprintf("%d", rev.RevisionNum),
			rev.CreatedAt.String(),
			rev.UserID.String(),
			rev.Source,
			rev.Description,
			applyGates,
		})
	}
	table.Render()
}

func apiListRevisions(spaceID string, unitID string, whereFilter string) ([]*goclientnew.Revision, error) {
	newParams := &goclientnew.ListExtendedRevisionsParams{}
	if whereFilter != "" {
		whereFilter = url.QueryEscape(whereFilter)
		newParams.Where = &whereFilter
	}
	revsRes, err := cubClientNew.ListExtendedRevisionsWithResponse(ctx,
		uuid.MustParse(spaceID),
		uuid.MustParse(unitID),
		newParams,
	)
	if IsAPIError(err, revsRes) {
		return nil, InterpretErrorGeneric(err, revsRes)
	}

	revisions := make([]*goclientnew.Revision, len(*revsRes.JSON200))
	for i, er := range *revsRes.JSON200 {
		revisions[i] = er.Revision
	}
	return revisions, nil
}
