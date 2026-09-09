// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var filterGetCmd = &cobra.Command{
	Use:   "get <slug or id>",
	Short: "Get details about a filter",
	Args:  cobra.ExactArgs(1),
	Long: getCommandHelp(`Get detailed information about a filter in a space including its ID, slug, display name, from type, where expressions, and resource type.

Examples:
`+"```"+`
  # Get details about a unit filter
  cub filter get --space my-space -o json unit-filter

  # Get details about a deployment filter
  cub filter get --space my-space -o json deployment-filter
`+"```"+`
`, ""),
	RunE: filterGetCmdRun,
}

func init() {
	addStandardGetFlags(filterGetCmd)
	enableOptionalSpace(filterGetCmd)
	filterCmd.AddCommand(filterGetCmd)
}

func filterGetCmdRun(cmd *cobra.Command, args []string) error {
	filterDetails, err := resolveFilter(args[0], selectedSpaceID, selectFields)
	if err != nil {
		return err
	}

	displayGetResults(filterDetails, displayExtendedFilterDetails)
	return nil
}

func displayFilterDetails(filterDetails *goclientnew.Filter) {
	// Create an ExtendedFilter wrapper with just the Filter set
	extendedFilter := &goclientnew.ExtendedFilter{
		Filter: filterDetails,
		// All other fields (Space, FromSpace, etc.) will be nil, causing Extended display to show IDs
	}
	displayExtendedFilterDetails(extendedFilter)
}

func displayExtendedFilterDetails(extendedFilter *goclientnew.ExtendedFilter) {
	filterDetails := extendedFilter.Filter
	view := tableView()
	view.Append([]string{"ID", filterDetails.FilterID.String()})
	view.Append([]string{"Name", filterDetails.Slug})

	// Show Space slug instead of Space ID when available
	if extendedFilter.Space != nil {
		view.Append([]string{"Space", extendedFilter.Space.Slug})
	} else {
		view.Append([]string{"Space ID", filterDetails.SpaceID.String()})
	}

	view.Append([]string{"Created At", filterDetails.CreatedAt.String()})
	view.Append([]string{"Updated At", filterDetails.UpdatedAt.String()})
	view.Append([]string{"Labels", labelsToString(filterDetails.Labels)})
	view.Append([]string{"Delete Gates", deleteGatesToString(filterDetails.DeleteGates)})
	view.Append([]string{"Annotations", annotationsToString(filterDetails.Annotations)})
	view.Append([]string{"Organization ID", filterDetails.OrganizationID.String()})
	view.Append([]string{"From", filterDetails.From})

	// Show From Space slug when available
	if extendedFilter.FromSpace != nil {
		view.Append([]string{"From Space", extendedFilter.FromSpace.Slug})
	} else if filterDetails.FromSpaceID != nil && *filterDetails.FromSpaceID != uuid.Nil {
		view.Append([]string{"From Space ID", filterDetails.FromSpaceID.String()})
	}

	if filterDetails.Where != "" {
		view.Append([]string{"Where", filterDetails.Where})
	}
	if filterDetails.WhereData != "" {
		view.Append([]string{"Where Data", filterDetails.WhereData})
	}
	if filterDetails.ResourceType != "" {
		view.Append([]string{"Resource Type", filterDetails.ResourceType})
	}
	if filterDetails.Hash != "" {
		view.Append([]string{"Hash", filterDetails.Hash})
	}
	view.Render()
}
