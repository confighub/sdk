// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var changesetListCmd = &cobra.Command{
	Use:   "list",
	Short: "List changesets",
	Long: getCommandHelp(`List changesets you have access to in a space or across all spaces.

Examples:
`+"```"+`
  # List all changesets in a space with headers
  cub changeset list --space my-space

  # List changesets across all spaces (requires --space "*")
  cub changeset list --space "*" --where "Description LIKE '%release%'"

  # List changesets without headers for scripting
  cub changeset list --space my-space --no-headers

  # List changesets in JSON format
  cub changeset list --space my-space -o json

  # List only changeset names
  cub changeset list --space my-space --no-headers -o name

  # List changesets with matching Descriptions
  cub changeset list --space my-space --where "Description LIKE 'Release%'"
`+"```"+`
`, ""),
	Args:        cobra.ExactArgs(0),
	RunE:        changesetListCmdRun,
	Annotations: map[string]string{"OrgLevel": ""},
}

// Default columns to display when no custom columns are specified
var defaultChangeSetColumns = []string{"ChangeSet.Slug", "Space.Slug", "ChangeSet.State", "StartTag.Slug", "EndTag.Slug", "ChangeSet.Description"}

// changesetListInclude is the Include parameter for change set list queries.
const changesetListInclude = "SpaceID,StartTagID,EndTagID"

// changesetBaseSelectFields are the fields always returned by change set list queries.
var changesetBaseSelectFields = []string{"Slug", "ChangeSetID", "SpaceID", "OrganizationID"}

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
	filterID, err := parseFilterFlag(filter)
	if err != nil {
		return err
	}

	extendedChangeSets, err := apiListChangeSets(selectedSpaceID, where, selectFields, filterID)
	if err != nil {
		return err
	}

	displayListResults(extendedChangeSets, getChangeSetSlug, displayChangeSetList)
	return nil
}

func getChangeSetSlug(changeset *goclientnew.ExtendedChangeSet) string {
	space := ""
	if changeset.Space != nil {
		space = changeset.Space.Slug
	}
	return prefixedSlug(space, changeset.ChangeSet.Slug)
}

func displayChangeSetList(changesets []*goclientnew.ExtendedChangeSet) {
	table := tableView()
	if !noheader {
		table.SetHeader([]string{"Name", "Space", "State", "Start-Tag", "End-Tag", "Description"})
	}
	for _, cs := range changesets {
		changeset := cs.ChangeSet
		spaceSlug := cs.ChangeSet.SpaceID.String()
		if cs.Space != nil {
			spaceSlug = cs.Space.Slug
		} else if selectedSpaceID != "*" {
			spaceSlug = selectedSpaceSlug
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
			changeset.State,
			startTagSlug,
			endTagSlug,
			descriptionDisplay,
		})
	}
	table.Render()
}

// apiListChangeSets lists change sets via the org-level endpoint, scoped to a
// single space by a SpaceID clause unless spaceID is "*" (list across all spaces).
func apiListChangeSets(spaceID string, whereFilter string, selectParam string, filterParam string) ([]*goclientnew.ExtendedChangeSet, error) {
	where := cubapi.NewWhere(whereFilter)
	if spaceID != "*" {
		where = where.SpaceID(goclientnew.UUID(uuid.MustParse(spaceID)))
	}
	return apiListAllChangeSets(where, selectParam, filterParam)
}

func apiListAllChangeSets(where cubapi.Where, selectParam string, filterParam string) ([]*goclientnew.ExtendedChangeSet, error) {
	selectValue := handleSelectParameter(selectParam, selectFields, func() string {
		return buildSelectList("ChangeSet", nil, changesetListInclude, defaultChangeSetColumns, changesetAliases, changesetCustomColumnDependencies, changesetBaseSelectFields)
	})
	return cubapi.ListChangeSets(ctx, cubClient, where, cubapi.ListOpts{
		Select:   cubapi.SelectFields(selectValue),
		Include:  changesetListInclude,
		Filter:   filterParam,
		Contains: contains,
	})
}
