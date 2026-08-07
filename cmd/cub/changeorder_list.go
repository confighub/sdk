// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var changeorderListCmd = &cobra.Command{
	Use:   "list",
	Short: "List changeorders",
	Long: getCommandHelp(`List changeorders you have access to in a space or across all spaces.

Examples:
`+"```"+`
  # List all changeorders in a space with headers
  cub changeorder list --space my-space

  # List changeorders across all spaces (requires --space "*")
  cub changeorder list --space "*" --where "Description LIKE '%release%'"

  # List changeorders without headers for scripting
  cub changeorder list --space my-space --no-headers

  # List changeorders in JSON format
  cub changeorder list --space my-space -o json

  # List only changeorder names
  cub changeorder list --space my-space --no-headers -o name

  # List changeorders with matching Descriptions
  cub changeorder list --space my-space --where "Description LIKE 'Release%'"
`+"```"+`
`, ""),
	Args:        cobra.ExactArgs(0),
	RunE:        changeorderListCmdRun,
	Annotations: map[string]string{"OrgLevel": ""},
}

// Default columns to display when no custom columns are specified
var defaultChangeOrderColumns = []string{"ChangeOrder.Slug", "Space.Slug", "ChangeOrder.UpdateType", "SpaceFilter.Slug", "StartTag.Slug", "EndTag.Slug", "ChangeOrder.Description"}

// changeorderListInclude is the Include parameter for change order list queries.
const changeorderListInclude = "SpaceID,StartTagID,EndTagID,SpaceFilterID"

// changeorderBaseSelectFields are the fields always returned by change order list queries.
var changeorderBaseSelectFields = []string{"Slug", "ChangeOrderID", "SpaceID", "OrganizationID"}

// ChangeOrder-specific aliases
var changeorderAliases = map[string]string{
	"Name": "ChangeOrder.Slug",
	"ID":   "ChangeOrder.ChangeOrderID",
}

// ChangeOrder custom column dependencies
var changeorderCustomColumnDependencies = map[string][]string{}

func init() {
	addStandardListFlags(changeorderListCmd)
	changeorderCmd.AddCommand(changeorderListCmd)
}

func changeorderListCmdRun(cmd *cobra.Command, args []string) error {
	filterID, err := parseFilterFlag(filter)
	if err != nil {
		return err
	}

	extendedChangeOrders, err := apiListChangeOrders(selectedSpaceID, where, selectFields, filterID)
	if err != nil {
		return err
	}

	displayListResults(extendedChangeOrders, getChangeOrderSlug, displayChangeOrderList)
	return nil
}

func getChangeOrderSlug(changeorder *goclientnew.ExtendedChangeOrder) string {
	space := ""
	if changeorder.Space != nil {
		space = changeorder.Space.Slug
	}
	return prefixedSlug(space, changeorder.ChangeOrder.Slug)
}

func displayChangeOrderList(changeorders []*goclientnew.ExtendedChangeOrder) {
	table := tableView()
	if !noheader {
		table.SetHeader([]string{"Name", "Space", "Update-Type", "Space-Filter", "Start-Tag", "End-Tag", "Description"})
	}
	for _, cs := range changeorders {
		changeorder := cs.ChangeOrder
		spaceSlug := cs.ChangeOrder.SpaceID.String()
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

		spaceFilterSlug := ""
		if cs.SpaceFilter != nil {
			spaceFilterSlug = cs.SpaceFilter.Slug
		}

		// Truncate long descriptions for display
		descriptionDisplay := changeorder.Description
		if len(descriptionDisplay) > 50 {
			descriptionDisplay = descriptionDisplay[:47] + "..."
		}

		table.Append([]string{
			changeorder.Slug,
			spaceSlug,
			changeorder.UpdateType,
			spaceFilterSlug,
			startTagSlug,
			endTagSlug,
			descriptionDisplay,
		})
	}
	table.Render()
}

// apiListChangeOrders lists change orders via the org-level endpoint, scoped to a
// single space by a SpaceID clause unless spaceID is "*" (list across all spaces).
func apiListChangeOrders(spaceID string, whereFilter string, selectParam string, filterParam string) ([]*goclientnew.ExtendedChangeOrder, error) {
	where := cubapi.NewWhere(whereFilter)
	if spaceID != "*" {
		where = where.SpaceID(goclientnew.UUID(uuid.MustParse(spaceID)))
	}
	return apiListAllChangeOrders(where, selectParam, filterParam)
}

func apiListAllChangeOrders(where cubapi.Where, selectParam string, filterParam string) ([]*goclientnew.ExtendedChangeOrder, error) {
	selectValue := handleSelectParameter(selectParam, selectFields, func() string {
		return buildSelectList("ChangeOrder", nil, changeorderListInclude, defaultChangeOrderColumns, changeorderAliases, changeorderCustomColumnDependencies, changeorderBaseSelectFields)
	})
	return cubapi.ListChangeOrders(ctx, cubClient, where, cubapi.ListOpts{
		Select:   cubapi.SelectFields(selectValue),
		Include:  changeorderListInclude,
		Filter:   filterParam,
		Contains: contains,
	})
}
