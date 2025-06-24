// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"net/url"

	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var setListCmd = &cobra.Command{
	Use:   "list",
	Short: "List sets",
	Long: `List sets you have access to in a space. The output includes display names, slugs, set IDs, space IDs, and organization IDs.

Examples:
  # List all sets in a space
  cub set list --space my-space

  # List sets without headers for scripting
  cub set list --space my-space --noheader

  # List only set slugs
  cub set list --space my-space --noheader --slugs-only

  # List sets in JSON format
  cub set list --space my-space --quiet --json

  # List sets with debug information
  cub set list --space my-space --quiet --json --debug

  # List sets with custom JQ filter
  cub set list --space my-space --quiet --jq '.[].SetID'

  # List sets matching a specific slug
  cub set list --space my-space --where "Slug = 'my-set'"

  # List sets with minimal output
  cub set list --space my-space --quiet`,
	Args: cobra.ExactArgs(0),
	RunE: setListCmdRun,
}

func init() {
	addStandardListFlags(setListCmd)
	setCmd.AddCommand(setListCmd)
}

func setListCmdRun(cmd *cobra.Command, args []string) error {
	sets, err := apiListSets(selectedSpaceID, where)
	if err != nil {
		return err
	}
	displayListResults(sets, getSetSlug, displaySetList)
	return nil
}

func getSetSlug(set *goclientnew.Set) string {
	return set.Slug
}

func displaySetList(sets []*goclientnew.Set) {
	table := tableView()
	if !noheader {
		table.SetHeader([]string{"Display-Name", "Slug", "ID", "Space-ID", "Org-ID"})
	}
	for _, set := range sets {
		table.Append([]string{
			set.DisplayName,
			set.Slug,
			set.SetID.String(),
			set.SpaceID.String(),
			set.OrganizationID.String(),
		})
	}
	table.Render()
}

func apiListSets(spaceID string, whereFilter string) ([]*goclientnew.Set, error) {
	newParams := goclientnew.ListSetsParams{}
	if whereFilter != "" {
		whereFilter = url.QueryEscape(whereFilter)
		newParams.Where = &whereFilter
	}

	setsRes, err := cubClientNew.ListSetsWithResponse(ctx, uuid.MustParse(spaceID), &newParams)
	if IsAPIError(err, setsRes) {
		return nil, InterpretErrorGeneric(err, setsRes)
	}

	sets := make([]*goclientnew.Set, 0, len(*setsRes.JSON200))
	for _, exSet := range *setsRes.JSON200 {
		sets = append(sets, exSet.Set)
	}

	return sets, nil
}
