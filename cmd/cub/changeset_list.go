// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var changesetListCmd = &cobra.Command{
	Use:   "list",
	Short: "List changesets",
	Long: `List changesets you have access to in a space or across all spaces.

Examples:
  # List all changesets in a space with headers
  cub changeset list --space my-space

  # List changesets across all spaces (requires --space "*")
  cub changeset list --space "*" --where "Description LIKE '%release%'"

  # List changesets without headers for scripting
  cub changeset list --space my-space --no-header

  # List changesets in JSON format
  cub changeset list --space my-space --json

  # List only changeset names
  cub changeset list --space my-space --no-header --names

  # List changesets with specific filters
  cub changeset list --space my-space --where "FilterID IS NOT NULL"

  # List changesets with tags
  cub changeset list --space my-space --where "StartTagID IS NOT NULL AND EndTagID IS NOT NULL"`,
	Args:        cobra.ExactArgs(0),
	RunE:        changesetListCmdRun,
	Annotations: map[string]string{"OrgLevel": ""},
}

// Default columns to display when no custom columns are specified
var defaultChangeSetColumns = []string{"ChangeSet.Slug", "Space.Slug", "Filter.Slug", "StartTag.Slug", "EndTag.Slug", "ChangeSet.Description"}

// ChangeSet-specific aliases
var changesetAliases = map[string]string{
	"Name": "ChangeSet.Slug",
	"ID":   "ChangeSet.ChangeSetID",
}

// ChangeSet custom column dependencies
var changesetCustomColumnDependencies = map[string][]string{}

func init() {
	addStandardListFlags(changesetListCmd)
	changesetCmd.AddCommand(changesetListCmd)
}

func changesetListCmdRun(cmd *cobra.Command, args []string) error {
	var extendedChangeSets []*goclientnew.ExtendedChangeSet
	var err error

	if selectedSpaceID == "*" {
		extendedChangeSets, err = apiSearchChangeSets(where, selectFields)
		if err != nil {
			return err
		}
	} else {
		extendedChangeSets, err = apiListChangeSets(selectedSpaceID, where, selectFields)
		if err != nil {
			return err
		}
	}

	displayListResults(extendedChangeSets, getChangeSetSlug, displayChangeSetList)
	return nil
}

func getChangeSetSlug(changeset *goclientnew.ExtendedChangeSet) string {
	return changeset.ChangeSet.Slug
}

func displayChangeSetList(changesets []*goclientnew.ExtendedChangeSet) {
	table := tableView()
	if !noheader {
		table.SetHeader([]string{"Name", "Space", "Filter", "Start-Tag", "End-Tag", "Description"})
	}
	for _, cs := range changesets {
		changeset := cs.ChangeSet
		spaceSlug := cs.ChangeSet.ChangeSetID.String()
		if cs.Space != nil {
			spaceSlug = cs.Space.Slug
		} else if selectedSpaceID != "*" {
			spaceSlug = selectedSpaceSlug
		}

		filterSlug := ""
		if cs.Filter != nil {
			filterSlug = cs.Filter.Slug
		}

		startTagSlug := ""
		if cs.StartTag != nil {
			startTagSlug = cs.StartTag.Slug
		}

		endTagSlug := ""
		if cs.EndTag != nil {
			endTagSlug = cs.EndTag.Slug
		}

		// Truncate long descriptions for display
		descriptionDisplay := changeset.Description
		if len(descriptionDisplay) > 50 {
			descriptionDisplay = descriptionDisplay[:47] + "..."
		}

		table.Append([]string{
			changeset.Slug,
			spaceSlug,
			filterSlug,
			startTagSlug,
			endTagSlug,
			descriptionDisplay,
		})
	}
	table.Render()
}

func apiListChangeSets(spaceID string, whereFilter string, selectParam string) ([]*goclientnew.ExtendedChangeSet, error) {
	newParams := &goclientnew.ListChangeSetsParams{}
	include := "SpaceID,FilterID,StartTagID,EndTagID"
	newParams.Include = &include
	if whereFilter != "" {
		newParams.Where = &whereFilter
	}
	if contains != "" {
		newParams.Contains = &contains
	}
	selectValue := handleSelectParameter(selectParam, selectFields, func() string {
		baseFields := []string{"Slug", "ChangeSetID", "SpaceID", "OrganizationID"}
		return buildSelectList("ChangeSet", "", include, defaultChangeSetColumns, changesetAliases, changesetCustomColumnDependencies, baseFields)
	})
	if selectValue != "" && selectValue != "*" {
		newParams.Select = &selectValue
	}
	changesetsRes, err := cubClientNew.ListChangeSetsWithResponse(ctx, uuid.MustParse(spaceID), newParams)
	if IsAPIError(err, changesetsRes) {
		return nil, InterpretErrorGeneric(err, changesetsRes)
	}

	changesets := make([]*goclientnew.ExtendedChangeSet, 0, len(*changesetsRes.JSON200))
	for _, changeset := range *changesetsRes.JSON200 {
		changesets = append(changesets, &changeset)
	}

	return changesets, nil
}

func apiSearchChangeSets(whereFilter string, selectParam string) ([]*goclientnew.ExtendedChangeSet, error) {
	newParams := &goclientnew.ListAllChangeSetsParams{}
	if whereFilter != "" {
		newParams.Where = &whereFilter
	}
	if contains != "" {
		newParams.Contains = &contains
	}

	include := "SpaceID,FilterID,StartTagID,EndTagID"
	newParams.Include = &include

	selectValue := handleSelectParameter(selectParam, selectFields, func() string {
		baseFields := []string{"Slug", "ChangeSetID", "SpaceID", "OrganizationID"}
		return buildSelectList("ChangeSet", "", include, defaultChangeSetColumns, changesetAliases, changesetCustomColumnDependencies, baseFields)
	})
	if selectValue != "" && selectValue != "*" {
		newParams.Select = &selectValue
	}

	res, err := cubClientNew.ListAllChangeSets(ctx, newParams)
	if err != nil {
		return nil, err
	}
	changesetsRes, err := goclientnew.ParseListAllChangeSetsResponse(res)
	if IsAPIError(err, changesetsRes) {
		return nil, InterpretErrorGeneric(err, changesetsRes)
	}

	extendedChangeSets := make([]*goclientnew.ExtendedChangeSet, 0, len(*changesetsRes.JSON200))
	for _, changeset := range *changesetsRes.JSON200 {
		extendedChangeSets = append(extendedChangeSets, &changeset)
	}

	return extendedChangeSets, nil
}