// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"strings"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var viewListCmd = &cobra.Command{
	Use:   "list",
	Short: "List views",
	Long: getCommandHelp(`List views you have access to in a space or across all spaces.

Examples:
`+"```"+`
  # List all views in a space with headers
  cub view list --space my-space

  # List views across all spaces (requires --space "*")
  cub view list --space "*" --where "FilterID IS NOT NULL"

  # List views without headers for scripting
  cub view list --space my-space --no-headers

  # List views in JSON format
  cub view list --space my-space -o json

  # List only view names
  cub view list --space my-space --no-headers -o name

  # List views with specific filters
  cub view list --space my-space --where "GroupBy IS NOT NULL"

  # List views with ordering
  cub view list --space my-space --where "OrderBy IS NOT NULL"
`+"```"+`
`, ""),
	Args:        cobra.ExactArgs(0),
	RunE:        viewListCmdRun,
	Annotations: map[string]string{"OrgLevel": ""},
}

// Default columns to display when no custom columns are specified
var defaultViewColumns = []string{"View.Slug", "Space.Slug", "Filter.Slug", "View.Columns", "View.GroupBy", "View.OrderBy"}

// viewListInclude is the Include parameter for view list queries.
const viewListInclude = "SpaceID,FilterID"

// viewBaseSelectFields are the fields always returned by view list queries.
var viewBaseSelectFields = []string{"Slug", "ViewID", "SpaceID", "OrganizationID"}

// View-specific aliases
var viewAliases = map[string]string{
	"Name": "View.Slug",
	"ID":   "View.ViewID",
}

// View custom column dependencies
var viewCustomColumnDependencies = map[string][]string{}

func init() {
	addStandardListFlags(viewListCmd)
	viewCmd.AddCommand(viewListCmd)
}

func viewListCmdRun(cmd *cobra.Command, args []string) error {
	filterID, err := parseFilterFlag(filter)
	if err != nil {
		return err
	}

	extendedViews, err := apiListViews(selectedSpaceID, where, selectFields, filterID)
	if err != nil {
		return err
	}

	displayListResults(extendedViews, getViewSlug, displayViewList)
	return nil
}

func getViewSlug(view *goclientnew.ExtendedView) string {
	space := ""
	if view.Space != nil {
		space = view.Space.Slug
	}
	return prefixedSlug(space, view.View.Slug)
}

func formatColumnsForDisplay(columns []goclientnew.Column) string {
	if len(columns) == 0 {
		return ""
	}

	columnNames := make([]string, len(columns))
	for i, col := range columns {
		columnNames[i] = col.Name
	}

	result := strings.Join(columnNames, ", ")
	if len(result) > 40 {
		return result[:37] + "..."
	}
	return result
}

func displayViewList(views []*goclientnew.ExtendedView) {
	table := tableView()
	if !noheader {
		table.SetHeader([]string{"Name", "Space", "Filter", "Columns", "Group-By", "Order-By"})
	}
	for _, v := range views {
		view := v.View
		spaceSlug := v.View.ViewID.String()
		if v.Space != nil {
			spaceSlug = v.Space.Slug
		} else if selectedSpaceID != "*" {
			spaceSlug = selectedSpaceSlug
		}

		filterSlug := ""
		if v.Filter != nil {
			filterSlug = v.Filter.Slug
		}

		columnsDisplay := formatColumnsForDisplay(view.Columns)

		orderByDisplay := view.OrderBy
		if view.OrderByDirection != "" && view.OrderByDirection != "OrderByDirectionNone" {
			orderByDisplay = fmt.Sprintf("%s %s", view.OrderBy, view.OrderByDirection)
		}

		table.Append([]string{
			view.Slug,
			spaceSlug,
			filterSlug,
			columnsDisplay,
			view.GroupBy,
			orderByDisplay,
		})
	}
	table.Render()
}

// apiListViews lists views via the org-level endpoint, scoped to a single space
// by a SpaceID clause unless spaceID is "*" (list across all spaces).
func apiListViews(spaceID string, whereFilter string, selectParam string, filterParam string) ([]*goclientnew.ExtendedView, error) {
	where := cubapi.NewWhere(whereFilter)
	if spaceID != "*" {
		where = where.SpaceID(goclientnew.UUID(uuid.MustParse(spaceID)))
	}
	return apiListAllViews(where, selectParam, filterParam)
}

func apiListAllViews(where cubapi.Where, selectParam string, filterParam string) ([]*goclientnew.ExtendedView, error) {
	selectValue := handleSelectParameter(selectParam, selectFields, func() string {
		return buildSelectList("View", nil, viewListInclude, defaultViewColumns, viewAliases, viewCustomColumnDependencies, viewBaseSelectFields)
	})
	return cubapi.ListViews(ctx, cubClient, where, cubapi.ListOpts{
		Select:   cubapi.SelectFields(selectValue),
		Include:  viewListInclude,
		Filter:   filterParam,
		Contains: contains,
	})
}
