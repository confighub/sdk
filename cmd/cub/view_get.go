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

var viewGetCmd = &cobra.Command{
	Use:   "get <slug or id>",
	Short: "Get details about a view",
	Args:  cobra.ExactArgs(1),
	Long: `Get detailed information about a view in a space including its ID, slug, display name, filter, columns, and ordering.

Examples:
  # Get details about a unit view
  cub view get --space my-space unit-view

  # Get details about a view in JSON format
  cub view get --space my-space --json summary-view
`,
	RunE: viewGetCmdRun,
}

func init() {
	addStandardGetFlags(viewGetCmd)
	viewCmd.AddCommand(viewGetCmd)
}

func viewGetCmdRun(cmd *cobra.Command, args []string) error {
	viewDetails, err := apiGetViewFromSlug(args[0], selectFields)
	if err != nil {
		return err
	}

	displayGetResults(viewDetails, displayViewDetails)
	return nil
}

func formatColumnsForDetails(columns []goclientnew.Column) string {
	if len(columns) == 0 {
		return ""
	}
	
	columnNames := make([]string, len(columns))
	for i, col := range columns {
		columnNames[i] = col.Name
	}
	
	return strings.Join(columnNames, ", ")
}

func displayViewDetails(viewDetails *goclientnew.View) {
	view := tableView()
	view.Append([]string{"ID", viewDetails.ViewID.String()})
	view.Append([]string{"Name", viewDetails.Slug})
	view.Append([]string{"Display Name", viewDetails.DisplayName})
	view.Append([]string{"Space ID", viewDetails.SpaceID.String()})
	view.Append([]string{"Created At", viewDetails.CreatedAt.String()})
	view.Append([]string{"Updated At", viewDetails.UpdatedAt.String()})
	view.Append([]string{"Labels", labelsToString(viewDetails.Labels)})
	view.Append([]string{"Annotations", annotationsToString(viewDetails.Annotations)})
	view.Append([]string{"Organization ID", viewDetails.OrganizationID.String()})
	view.Append([]string{"Filter ID", viewDetails.FilterID.String()})
	
	if len(viewDetails.Columns) > 0 {
		view.Append([]string{"Columns", formatColumnsForDetails(viewDetails.Columns)})
		view.Append([]string{"Column Count", fmt.Sprintf("%d", len(viewDetails.Columns))})
	}
	
	if viewDetails.GroupBy != "" {
		view.Append([]string{"Group By", viewDetails.GroupBy})
	}
	
	if viewDetails.OrderBy != "" {
		view.Append([]string{"Order By", viewDetails.OrderBy})
		if viewDetails.OrderByDirection != "" && viewDetails.OrderByDirection != "OrderByDirectionNone" {
			view.Append([]string{"Order By Direction", string(viewDetails.OrderByDirection)})
		}
	}
	
	view.Render()
}

func apiGetView(viewID string, selectParam string) (*goclientnew.View, error) {
	newParams := &goclientnew.GetViewParams{}
	include := "FilterID"
	newParams.Include = &include
	selectValue := handleSelectParameter(selectParam, selectFields, nil)
	if selectValue != "" && selectValue != "*" {
		newParams.Select = &selectValue
	}
	viewRes, err := cubClientNew.GetViewWithResponse(ctx, uuid.MustParse(selectedSpaceID), uuid.MustParse(viewID), newParams)
	if IsAPIError(err, viewRes) {
		return nil, InterpretErrorGeneric(err, viewRes)
	}
	return viewRes.JSON200.View, nil
}

func apiGetViewFromSlug(slug string, selectParam string) (*goclientnew.View, error) {
	id, err := uuid.Parse(slug)
	if err == nil {
		return apiGetView(id.String(), selectParam)
	}
	// The default for get is "*" rather than auto-selected list columns
	if selectParam == "" {
		selectParam = "*"
	}
	views, err := apiListViews(selectedSpaceID, "Slug = '"+slug+"'", selectParam)
	if err != nil {
		return nil, err
	}
	// find view by slug
	for _, view := range views {
		if view.View != nil && view.View.Slug == slug {
			return view.View, nil
		}
	}
	return nil, fmt.Errorf("view %s not found in space %s", slug, selectedSpaceSlug)
}