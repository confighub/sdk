// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"net/url"

	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/spf13/cobra"
)

var spaceListCmd = &cobra.Command{
	Use:   "list",
	Short: "List spaces",
	Long: `List spaces you have access to in this organization. The output includes display names, slugs, space IDs, and organization IDs.

Examples:
  # List all spaces with headers
  cub space list

  # List spaces without headers for scripting
  cub space list --noheader

  # List spaces in JSON format
  cub space list --json

  # List spaces with custom JQ filter
  cub space list --jq '.[].Slug'

  # List spaces matching a specific criteria
  cub space list --where "DisplayName contains 'prod'"`,
	RunE: spaceListCmdRun,
}

func init() {
	addStandardListFlags(spaceListCmd)
	spaceCmd.AddCommand(spaceListCmd)
}

func spaceListCmdRun(cmd *cobra.Command, args []string) error {
	extendedSpaces, err := apiListExtendedSpaces(where)
	if err != nil {
		return err
	}
	displayListResults(extendedSpaces, getExtendedSpaceSlug, displayExtendedSpaceList)
	return nil
}

func getExtendedSpaceSlug(extendedSpace *goclientnew.ExtendedSpace) string {
	return extendedSpace.Space.Slug
}

func displayExtendedSpaceList(extendedSpaces []*goclientnew.ExtendedSpace) {
	table := tableView()
	if !noheader {
		table.SetHeader([]string{"Display-Name", "Slug", "ID", "#Units", "#Gated", "#Upgradable", "#Workers", "#Targets", "#Triggers"})
	}
	for _, extendedSpace := range extendedSpaces {
		table.Append([]string{
			extendedSpace.Space.DisplayName,
			extendedSpace.Space.Slug,
			extendedSpace.Space.SpaceID.String(),
			fmt.Sprintf("%d", extendedSpace.TotalUnitCount),
			fmt.Sprintf("%d", extendedSpace.GatedUnitCount),
			fmt.Sprintf("%d", extendedSpace.UpgradableUnitCount),
			fmt.Sprintf("%d", extendedSpace.TotalBridgeWorkerCount),
			fmt.Sprintf("%d", totalCountMap(extendedSpace.TargetCountByToolchainType)),
			fmt.Sprintf("%d", totalCountMap(extendedSpace.TriggerCountByEventType)),
		})
	}
	table.Render()
}

func apiListSpaces(whereFilter string) ([]*goclientnew.Space, error) {
	newParams := &goclientnew.ListSpacesParams{}
	if whereFilter != "" {
		whereFilter = url.QueryEscape(whereFilter)
		newParams.Where = &whereFilter
	}
	spacesRes, err := cubClientNew.ListSpacesWithResponse(ctx, newParams)
	if IsAPIError(err, spacesRes) {
		return nil, InterpretErrorGeneric(err, spacesRes)
	}

	spaces := make([]*goclientnew.Space, 0, len(*spacesRes.JSON200))
	for _, extendedSpace := range *spacesRes.JSON200 {
		if extendedSpace.Space != nil {
			spaces = append(spaces, extendedSpace.Space)
		}
	}

	return spaces, nil
}

func apiListExtendedSpaces(whereFilter string) ([]*goclientnew.ExtendedSpace, error) {
	newParams := &goclientnew.ListSpacesParams{}
	summary := true
	newParams.Summary = &summary
	if whereFilter != "" {
		whereFilter = url.QueryEscape(whereFilter)
		newParams.Where = &whereFilter
	}
	spacesRes, err := cubClientNew.ListSpacesWithResponse(ctx, newParams)
	if IsAPIError(err, spacesRes) {
		return nil, InterpretErrorGeneric(err, spacesRes)
	}

	extendedSpaces := make([]*goclientnew.ExtendedSpace, 0, len(*spacesRes.JSON200))
	for _, extendedSpace := range *spacesRes.JSON200 {
		if extendedSpace.Space != nil {
			extendedSpaces = append(extendedSpaces, &extendedSpace)
		}
	}

	return extendedSpaces, nil
}
