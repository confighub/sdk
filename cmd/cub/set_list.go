// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var setListCmd = &cobra.Command{
	Use:   "list",
	Short: "List sets",
	Long: `List sets you have access to in a space. The output includes slugs and space slugs.

Examples:
  # List all sets in a space
  cub set list --space my-space

  # List sets without headers for scripting
  cub set list --space my-space --no-header

  # List only set names
  cub set list --space my-space --no-header --names

  # List sets in JSON format
  cub set list --space my-space --quiet --json

  # List sets with debug information
  cub set list --space my-space --quiet --json --debug

  # List sets with custom JQ filter
  cub set list --space my-space --quiet --jq '.[].Slug'

  # List sets matching a specific slug
  cub set list --space my-space --where "Slug = 'my-set'"

  # List sets with minimal output
  cub set list --space my-space --quiet`,
	Args: cobra.ExactArgs(0),
	RunE: setListCmdRun,
}

// Default columns to display when no custom columns are specified
var defaultSetColumns = []string{"Set.Slug", "Space.Slug"}

// Set-specific aliases
var setAliases = map[string]string{
	"Name": "Set.Slug",
	"ID":   "Set.SetID",
}

// Set custom column dependencies
var setCustomColumnDependencies = map[string][]string{}

func init() {
	addStandardListFlags(setListCmd)
	setCmd.AddCommand(setListCmd)
}

func setListCmdRun(cmd *cobra.Command, args []string) error {
	sets, err := apiListSets(selectedSpaceID, where, selectFields)
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
		table.SetHeader([]string{"Name", "Space"})
	}
	for _, set := range sets {
		table.Append([]string{
			set.Slug,
			selectedSpaceSlug,
		})
	}
	table.Render()
}

func apiListSets(spaceID string, whereFilter string, selectParam string) ([]*goclientnew.Set, error) {
	newParams := goclientnew.ListSetsParams{}
	if whereFilter != "" {
		newParams.Where = &whereFilter
	}
	if contains != "" {
		newParams.Contains = &contains
	}
	selectValue := handleSelectParameter(selectParam, selectFields, func() string {
		baseFields := []string{"Slug", "SetID", "SpaceID", "OrganizationID"}
		return buildSelectList("Set", "", "", defaultSetColumns, setAliases, setCustomColumnDependencies, baseFields)
	})
	if selectValue != "" && selectValue != "*" {
		newParams.Select = &selectValue
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
