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

var viewGetCmd = &cobra.Command{
	Use:   "get <slug or id>",
	Short: "Get details about a view",
	Args:  cobra.ExactArgs(1),
	Long: getCommandHelp(`Get detailed information about a view in a space including its ID, slug, display name, filter, columns, and ordering.

Examples:
`+"```"+`
  # Get details about a unit view
  cub view get --space my-space unit-view

  # Get details about a view in JSON format
  cub view get --space my-space --json summary-view
`+"```"+`
`, ""),
	RunE: viewGetCmdRun,
}

func init() {
	addStandardGetFlags(viewGetCmd)
	viewCmd.AddCommand(viewGetCmd)
}

func viewGetCmdRun(cmd *cobra.Command, args []string) error {
	viewDetails, err := apiGetExtendedViewFromSlug(args[0], selectFields)
	if err != nil {
		return err
	}

	displayGetResults(viewDetails, displayExtendedViewDetails)
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

func formatColumnDetail(col goclientnew.Column) string {
	parts := []string{col.Name}
	if col.ColumnType != "" {
		parts = append(parts, fmt.Sprintf("type=%s", col.ColumnType))
	}
	if col.DataType != "" {
		parts = append(parts, fmt.Sprintf("dataType=%s", col.DataType))
	}
	if col.ColumnSource != nil {
		if col.ColumnSource.MetadataAttribute != "" {
			parts = append(parts, fmt.Sprintf("source=%s", col.ColumnSource.MetadataAttribute))
		} else if col.ColumnSource.MetadataExpression != "" {
			parts = append(parts, fmt.Sprintf("expr=%s", col.ColumnSource.MetadataExpression))
		} else if col.ColumnSource.DataPath != nil {
			src := string(col.ColumnSource.DataPath.Path)
			if col.ColumnSource.DataPath.WhereResource != "" {
				src += " where " + col.ColumnSource.DataPath.WhereResource
			}
			parts = append(parts, fmt.Sprintf("path=%s", src))
		} else if col.ColumnSource.DataExpression != "" {
			parts = append(parts, fmt.Sprintf("expr=%s", col.ColumnSource.DataExpression))
		}
	}
	if col.GroupBy {
		parts = append(parts, "groupBy")
	}
	if col.OrderByDirection != "" {
		parts = append(parts, fmt.Sprintf("orderBy=%s", col.OrderByDirection))
	}
	return strings.Join(parts, " ")
}

func displayViewDetails(viewDetails *goclientnew.View) {
	// Create an ExtendedView wrapper with just the View set
	extendedView := &goclientnew.ExtendedView{
		View: viewDetails,
		// All other fields (Space, Filter, etc.) will be nil, causing Extended display to show IDs
	}
	displayExtendedViewDetails(extendedView)
}

func displayExtendedViewDetails(extendedView *goclientnew.ExtendedView) {
	viewDetails := extendedView.View
	view := tableView()
	view.Append([]string{"ID", viewDetails.ViewID.String()})
	view.Append([]string{"Name", viewDetails.Slug})

	// Show Space slug instead of Space ID when available
	if extendedView.Space != nil {
		view.Append([]string{"Space", extendedView.Space.Slug})
	} else {
		view.Append([]string{"Space ID", viewDetails.SpaceID.String()})
	}

	view.Append([]string{"Created At", viewDetails.CreatedAt.String()})
	view.Append([]string{"Updated At", viewDetails.UpdatedAt.String()})
	view.Append([]string{"Labels", labelsToString(viewDetails.Labels)})
	view.Append([]string{"Delete Gates", deleteGatesToString(viewDetails.DeleteGates)})
	view.Append([]string{"Annotations", annotationsToString(viewDetails.Annotations)})
	view.Append([]string{"Organization ID", viewDetails.OrganizationID.String()})

	// Show Filter slug when available
	if extendedView.Filter != nil {
		view.Append([]string{"Filter", extendedView.Filter.Slug})
	} else if viewDetails.FilterID != nil {
		view.Append([]string{"Filter ID", viewDetails.FilterID.String()})
	}

	if viewDetails.Of != "" {
		view.Append([]string{"Of", viewDetails.Of})
	}

	if len(viewDetails.Columns) > 0 {
		// Check if any columns have enhanced fields
		hasEnhanced := false
		for _, col := range viewDetails.Columns {
			if col.ColumnType != "" || col.ColumnSource != nil {
				hasEnhanced = true
				break
			}
		}
		if hasEnhanced {
			for i, col := range viewDetails.Columns {
				label := fmt.Sprintf("Column %d", i+1)
				view.Append([]string{label, formatColumnDetail(col)})
			}
		} else {
			view.Append([]string{"Columns", formatColumnsForDetails(viewDetails.Columns)})
		}
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
	extendedView, err := apiGetExtendedView(viewID, selectParam)
	if err != nil {
		return nil, err
	}
	return extendedView.View, nil
}

func apiGetExtendedView(viewID string, selectParam string) (*goclientnew.ExtendedView, error) {
	newParams := &goclientnew.GetViewParams{}
	include := "SpaceID,FilterID"
	newParams.Include = &include
	selectValue := handleSelectParameter(selectParam, selectFields, nil)
	if selectValue != "" && selectValue != "*" {
		newParams.Select = &selectValue
	}
	viewRes, err := cubClientNew.GetViewWithResponse(ctx, uuid.MustParse(selectedSpaceID), uuid.MustParse(viewID), newParams)
	if cubapi.IsAPIError(err, viewRes) {
		return nil, cubapi.InterpretErrorGeneric(err, viewRes)
	}
	return viewRes.JSON200, nil
}

func apiGetViewFromSlug(slug string, selectParam string) (*goclientnew.View, error) {
	return apiGetViewFromSlugInSpace(slug, selectedSpaceID, selectParam)
}

func apiGetExtendedViewFromSlug(slug string, selectParam string) (*goclientnew.ExtendedView, error) {
	id, err := uuid.Parse(slug)
	if err == nil {
		return apiGetExtendedView(id.String(), selectParam)
	}
	// The default for get is "*" rather than auto-selected list columns
	if selectParam == "" {
		selectParam = "*"
	}
	views, err := apiListViews(selectedSpaceID, "Slug = '"+slug+"'", selectParam, "")
	if err != nil {
		return nil, err
	}
	// find view by slug
	for _, view := range views {
		if view.View != nil && view.View.Slug == slug {
			return view, nil
		}
	}
	return nil, fmt.Errorf("view %s not found in space %s", slug, selectedSpaceID)
}

func apiGetViewFromSlugInSpace(slug string, spaceID string, selectParam string) (*goclientnew.View, error) {
	id, err := uuid.Parse(slug)
	if err == nil {
		return apiGetView(id.String(), selectParam)
	}
	// The default for get is "*" rather than auto-selected list columns
	if selectParam == "" {
		selectParam = "*"
	}
	views, err := apiListViews(spaceID, "Slug = '"+slug+"'", selectParam, "")
	if err != nil {
		return nil, err
	}
	// find view by slug
	for _, view := range views {
		if view.View != nil && view.View.Slug == slug {
			return view.View, nil
		}
	}

	// Get space slug for error message
	spaceSlug := spaceID
	if spaceUUID, err := uuid.Parse(spaceID); err == nil {
		if space, err := apiGetSpace(spaceUUID.String(), "Slug"); err == nil && space != nil {
			spaceSlug = space.Slug
		}
	}
	return nil, fmt.Errorf("view %s not found in space %s", slug, spaceSlug)
}
