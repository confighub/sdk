// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"sort"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
)

var revisionListCmd = &cobra.Command{
	Use:   "list <unit>",
	Short: "List revisions",
	Long: `List revisions for a unit in a space. The output includes revision numbers, timestamps, usernames, sources, descriptions, and apply gates. Revisions track the history of changes made to a unit's configuration.

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

func getRevisionSlug(extendedRevision *goclientnew.ExtendedRevision) string {
	// Use number
	return fmt.Sprintf("%d", extendedRevision.Revision.RevisionNum)
}

func displayRevisionList(extendedRevisions []*goclientnew.ExtendedRevision) {
	table := tableView()
	if !noheader {
		table.SetHeader([]string{"Num", "Time", "User", "Source", "Description", "Apply-Gates"})
	}
	for _, extendedRev := range extendedRevisions {
		rev := extendedRev.Revision
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
		username := ""
		if extendedRev.User != nil {
			username = extendedRev.User.Username
		}
		table.Append([]string{
			fmt.Sprintf("%d", rev.RevisionNum),
			rev.CreatedAt.String(),
			username,
			rev.Source,
			rev.Description,
			applyGates,
		})
	}
	table.Render()
}

func apiListRevisions(spaceID string, unitID string, whereFilter string) ([]*goclientnew.ExtendedRevision, error) {
	newParams := &goclientnew.ListExtendedRevisionsParams{}
	if whereFilter != "" {
		newParams.Where = &whereFilter
	}
	include := "UserID"
	newParams.Include = &include
	revsRes, err := cubClientNew.ListExtendedRevisionsWithResponse(ctx,
		uuid.MustParse(spaceID),
		uuid.MustParse(unitID),
		newParams,
	)
	if IsAPIError(err, revsRes) {
		return nil, InterpretErrorGeneric(err, revsRes)
	}

	revisions := make([]*goclientnew.ExtendedRevision, len(*revsRes.JSON200))
	for i, er := range *revsRes.JSON200 {
		revisions[i] = &er
	}
	
	// Sort by RevisionNum descending
	sort.Slice(revisions, func(i, j int) bool {
		return revisions[i].Revision.RevisionNum > revisions[j].Revision.RevisionNum
	})
	
	return revisions, nil
}
