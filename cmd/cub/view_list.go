// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"strings"

	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var viewListCmd = &cobra.Command{
	Use:   "list",
	Short: "List views",
	Long: `List views you have access to in a space or across all spaces.

Examples:
  # List all views in a space with headers
  cub view list --space my-space

  # List views across all spaces (requires --space "*")
  cub view list --space "*" --where "FilterID IS NOT NULL"

  # List views without headers for scripting
  cub view list --space my-space --no-header

  # List views in JSON format
  cub view list --space my-space --json

  # List only view names
  cub view list --space my-space --no-header --names

  # List views with specific filters
  cub view list --space my-space --where "GroupBy IS NOT NULL"

  # List views with ordering
  cub view list --space my-space --where "OrderBy IS NOT NULL"`,
	Args:        cobra.ExactArgs(0),
	RunE:        viewListCmdRun,
	Annotations: map[string]string{"OrgLevel": ""},
}

// Default columns to display when no custom columns are specified
var defaultViewColumns = []string{"View.Slug", "Space.Slug", "Filter.Slug", "View.Columns", "View.GroupBy", "View.OrderBy"}

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
	var extendedViews []*goclientnew.ExtendedView
	var err error

	if selectedSpaceID == "*" {
		extendedViews, err = apiSearchViews(where, selectFields)
		if err != nil {
			return err
		}
	} else {
		extendedViews, err = apiListViews(selectedSpaceID, where, selectFields)
		if err != nil {
			return err
		}
	}

	displayListResults(extendedViews, getViewSlug, displayViewList)
	return nil
}

func getViewSlug(view *goclientnew.ExtendedView) string {
	return view.View.Slug
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

func apiListViews(spaceID string, whereFilter string, selectParam string) ([]*goclientnew.ExtendedView, error) {
	newParams := &goclientnew.ListViewsParams{}
	include := "SpaceID,FilterID"
	newParams.Include = &include
	if whereFilter != "" {
		newParams.Where = &whereFilter
	}
	if contains != "" {
		newParams.Contains = &contains
	}
	selectValue := handleSelectParameter(selectParam, selectFields, func() string {
		baseFields := []string{"Slug", "ViewID", "SpaceID", "OrganizationID"}
		return buildSelectList("View", "", include, defaultViewColumns, viewAliases, viewCustomColumnDependencies, baseFields)
	})
	if selectValue != "" && selectValue != "*" {
		newParams.Select = &selectValue
	}
	viewsRes, err := cubClientNew.ListViewsWithResponse(ctx, uuid.MustParse(spaceID), newParams)
	if IsAPIError(err, viewsRes) {
		return nil, InterpretErrorGeneric(err, viewsRes)
	}

	views := make([]*goclientnew.ExtendedView, 0, len(*viewsRes.JSON200))
	for _, view := range *viewsRes.JSON200 {
		views = append(views, &view)
	}

	return views, nil
}

func apiSearchViews(whereFilter string, selectParam string) ([]*goclientnew.ExtendedView, error) {
	newParams := &goclientnew.ListAllViewsParams{}
	if whereFilter != "" {
		newParams.Where = &whereFilter
	}
	if contains != "" {
		newParams.Contains = &contains
	}

	include := "SpaceID,FilterID"
	newParams.Include = &include

	selectValue := handleSelectParameter(selectParam, selectFields, func() string {
		baseFields := []string{"Slug", "ViewID", "SpaceID", "OrganizationID"}
		return buildSelectList("View", "", include, defaultViewColumns, viewAliases, viewCustomColumnDependencies, baseFields)
	})
	if selectValue != "" && selectValue != "*" {
		newParams.Select = &selectValue
	}

	res, err := cubClientNew.ListAllViews(ctx, newParams)
	if err != nil {
		return nil, err
	}
	viewsRes, err := goclientnew.ParseListAllViewsResponse(res)
	if IsAPIError(err, viewsRes) {
		return nil, InterpretErrorGeneric(err, viewsRes)
	}

	extendedViews := make([]*goclientnew.ExtendedView, 0, len(*viewsRes.JSON200))
	for _, view := range *viewsRes.JSON200 {
		extendedViews = append(extendedViews, &view)
	}

	return extendedViews, nil
}