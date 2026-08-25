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
	Long: getCommandHelp(`List changeorders you have access to in a space or across all spaces. -o wide adds the start and end tags.

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

  # List the changeorders a space has taken, or has released
  cub changeorder list --space my-space --where "ResolvedSpaceIDs ? '<space-id>'"
  cub changeorder list --space my-space --where "ReleasedSpaceIDs ? '<space-id>'"

  # List the changeorders still on their way, and the ones given up on
  cub changeorder list --space "*" --where "State = 'InProgress'"
  cub changeorder list --space "*" --where "State = 'Aborted'"
`+"```"+`
`, ""),
	Args:        cobra.ExactArgs(0),
	RunE:        changeorderListCmdRun,
	Annotations: map[string]string{"OrgLevel": ""},
}

// Default columns to display when no custom columns are specified. This is also what drives the
// select list, so it names every field either layout shows -- the wide-only tags included.
var defaultChangeOrderColumns = []string{"ChangeOrder.Slug", "Space.Slug", "ChangeOrder.State", "ChangeOrder.UpdateType", "StartTag.Slug", "EndTag.Slug", "ChangeOrder.Description", "ChangeOrder.AbortedReason"}

// changeorderListInclude is the Include parameter for change order list queries.
const changeorderListInclude = "SpaceID,StartTagID,EndTagID"

// changeorderBaseSelectFields are the fields always returned by change order list queries.
// AbortedReason and InScopeSpaceIDs are among them because State is derived from both, and each
// is legitimately empty -- so the server has to re-read a change order whose select left either
// out, rather than reading a missing value as a real one.
var changeorderBaseSelectFields = []string{"Slug", "ChangeOrderID", "SpaceID", "OrganizationID", "AbortedReason", "InScopeSpaceIDs"}

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

// displayChangeOrderList renders the table. The start and end tags are named after the ChangeSet
// they came from, so the default layout leaves them out and -o wide shows them.
func displayChangeOrderList(changeorders []*goclientnew.ExtendedChangeOrder) {
	wide := effectiveOutput().Kind == OutputWide
	table := tableView()
	if !noheader {
		header := []string{"Name", "Space", "State", "Update-Type"}
		if wide {
			header = append(header, "Start-Tag", "End-Tag")
		}
		header = append(header, "Description", "Aborted-Reason")
		table.SetHeader(header)
	}
	for _, cs := range changeorders {
		changeorder := cs.ChangeOrder
		spaceSlug := cs.ChangeOrder.SpaceID.String()
		if cs.Space != nil {
			spaceSlug = cs.Space.Slug
		} else if selectedSpaceID != "*" {
			spaceSlug = selectedSpaceSlug
		}

		row := []string{
			changeorder.Slug,
			spaceSlug,
			changeorder.State,
			changeorder.UpdateType,
		}
		if wide {
			startTagSlug := ""
			if cs.StartTag != nil {
				startTagSlug = cs.StartTag.Slug
			}

			endTagSlug := ""
			if cs.EndTag != nil {
				endTagSlug = cs.EndTag.Slug
			}

			row = append(row, startTagSlug, endTagSlug)
		}
		row = append(row,
			truncateWithEllipsis(changeorder.Description, defaultColumnWidth),
			truncateWithEllipsis(changeorder.AbortedReason, defaultColumnWidth),
		)

		table.Append(row)
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
