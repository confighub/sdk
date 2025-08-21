// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var filterListCmd = &cobra.Command{
	Use:   "list",
	Short: "List filters",
	Long: `List filters you have access to in a space or across all spaces.

Examples:
  # List all filters in a space with headers
  cub filter list --space my-space

  # List filters across all spaces (requires --space "*")
  cub filter list --space "*" --where "From = 'Unit'"

  # List filters without headers for scripting
  cub filter list --space my-space --no-header

  # List filters in JSON format
  cub filter list --space my-space --json

  # List only filter names
  cub filter list --space my-space --no-header --names

  # List filters with a specific From type
  cub filter list --space my-space --where "From = 'Unit'"

  # List filters with resource type
  cub filter list --space my-space --where "ResourceType LIKE 'apps/v1/%'"`,
	Args:        cobra.ExactArgs(0),
	RunE:        filterListCmdRun,
	Annotations: map[string]string{"OrgLevel": ""},
}

// Default columns to display when no custom columns are specified
var defaultFilterColumns = []string{"Filter.Slug", "Space.Slug", "Filter.From", "Filter.Where", "Filter.WhereData", "Filter.ResourceType", "FromSpace.Slug"}

// Filter-specific aliases
var filterAliases = map[string]string{
	"Name": "Filter.Slug",
	"ID":   "Filter.FilterID",
}

// Filter custom column dependencies
var filterCustomColumnDependencies = map[string][]string{}

func init() {
	addStandardListFlags(filterListCmd)
	filterCmd.AddCommand(filterListCmd)
}

func filterListCmdRun(cmd *cobra.Command, args []string) error {
	var extendedFilters []*goclientnew.ExtendedFilter
	var err error

	if selectedSpaceID == "*" {
		extendedFilters, err = apiSearchFilters(where, selectFields)
		if err != nil {
			return err
		}
	} else {
		extendedFilters, err = apiListFilters(selectedSpaceID, where, selectFields)
		if err != nil {
			return err
		}
	}

	displayListResults(extendedFilters, getFilterSlug, displayFilterList)
	return nil
}

func getFilterSlug(filter *goclientnew.ExtendedFilter) string {
	return filter.Filter.Slug
}

func displayFilterList(filters []*goclientnew.ExtendedFilter) {
	table := tableView()
	if !noheader {
		table.SetHeader([]string{"Name", "Space", "From", "Where", "Where-Data", "Resource-Type", "From-Space"})
	}
	for _, f := range filters {
		filter := f.Filter
		spaceSlug := f.Filter.FilterID.String()
		if f.Space != nil {
			spaceSlug = f.Space.Slug
		} else if selectedSpaceID != "*" {
			spaceSlug = selectedSpaceSlug
		}
		
		fromSpaceSlug := ""
		if f.FromSpace != nil {
			fromSpaceSlug = f.FromSpace.Slug
		}
		
		// Truncate long where clauses for display
		whereDisplay := filter.Where
		if len(whereDisplay) > 50 {
			whereDisplay = whereDisplay[:47] + "..."
		}
		
		whereDataDisplay := filter.WhereData
		if len(whereDataDisplay) > 30 {
			whereDataDisplay = whereDataDisplay[:27] + "..."
		}
		
		table.Append([]string{
			filter.Slug,
			spaceSlug,
			filter.From,
			whereDisplay,
			whereDataDisplay,
			filter.ResourceType,
			fromSpaceSlug,
		})
	}
	table.Render()
}

func apiListFilters(spaceID string, whereFilter string, selectParam string) ([]*goclientnew.ExtendedFilter, error) {
	newParams := &goclientnew.ListFiltersParams{}
	include := "SpaceID,FromSpaceID"
	newParams.Include = &include
	if whereFilter != "" {
		newParams.Where = &whereFilter
	}
	if contains != "" {
		newParams.Contains = &contains
	}
	selectValue := handleSelectParameter(selectParam, selectFields, func() string {
		baseFields := []string{"Slug", "FilterID", "SpaceID", "OrganizationID"}
		return buildSelectList("Filter", "", include, defaultFilterColumns, filterAliases, filterCustomColumnDependencies, baseFields)
	})
	if selectValue != "" && selectValue != "*" {
		newParams.Select = &selectValue
	}
	filtersRes, err := cubClientNew.ListFiltersWithResponse(ctx, uuid.MustParse(spaceID), newParams)
	if IsAPIError(err, filtersRes) {
		return nil, InterpretErrorGeneric(err, filtersRes)
	}

	filters := make([]*goclientnew.ExtendedFilter, 0, len(*filtersRes.JSON200))
	for _, filter := range *filtersRes.JSON200 {
		filters = append(filters, &filter)
	}

	return filters, nil
}

func apiSearchFilters(whereFilter string, selectParam string) ([]*goclientnew.ExtendedFilter, error) {
	newParams := &goclientnew.ListAllFiltersParams{}
	if whereFilter != "" {
		newParams.Where = &whereFilter
	}
	if contains != "" {
		newParams.Contains = &contains
	}

	include := "SpaceID,FromSpaceID"
	newParams.Include = &include

	selectValue := handleSelectParameter(selectParam, selectFields, func() string {
		baseFields := []string{"Slug", "FilterID", "SpaceID", "OrganizationID"}
		return buildSelectList("Filter", "", include, defaultFilterColumns, filterAliases, filterCustomColumnDependencies, baseFields)
	})
	if selectValue != "" && selectValue != "*" {
		newParams.Select = &selectValue
	}

	res, err := cubClientNew.ListAllFilters(ctx, newParams)
	if err != nil {
		return nil, err
	}
	filtersRes, err := goclientnew.ParseListAllFiltersResponse(res)
	if IsAPIError(err, filtersRes) {
		return nil, InterpretErrorGeneric(err, filtersRes)
	}

	extendedFilters := make([]*goclientnew.ExtendedFilter, 0, len(*filtersRes.JSON200))
	for _, filter := range *filtersRes.JSON200 {
		extendedFilters = append(extendedFilters, &filter)
	}

	return extendedFilters, nil
}