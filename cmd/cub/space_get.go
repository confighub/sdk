// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"

	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

var spaceGetCmd = &cobra.Command{
	Use:   "get <name or id>",
	Short: "Get details about a space",
	Args:  cobra.ExactArgs(1),
	Long: `Get detailed information about a space including its ID, name, and organization details.

Examples:
  # Get space details in table format
  cub space get my-space

  # Get space details in JSON format
  cub space get --json my-space

`,
	RunE: spaceGetCmdRun,
}

func init() {
	addStandardGetFlags(spaceGetCmd)
	spaceCmd.AddCommand(spaceGetCmd)
}

func spaceGetCmdRun(cmd *cobra.Command, args []string) error {
	spaceDetails, err := apiGetSpaceFromSlug(args[0])
	if err != nil {
		return err
	}

	// the previous call got the list resource. We want the "detail" resource just in case they're different
	extendedSpace, err := apiGetExtendedSpace(spaceDetails.SpaceID.String())
	if err != nil {
		return err
	}
	displayGetResults(extendedSpace, displayExtendedSpaceDetails)
	return nil
}

func displaySpaceDetailsInView(spaceDetails *goclientnew.Space, view *tablewriter.Table) {
	view.Append([]string{"ID", spaceDetails.SpaceID.String()})
	view.Append([]string{"Name", spaceDetails.Slug})
	view.Append([]string{"Created At", spaceDetails.CreatedAt.String()})
	view.Append([]string{"Updated At", spaceDetails.UpdatedAt.String()})
	view.Append([]string{"Labels", labelsToString(spaceDetails.Labels)})
	view.Append([]string{"Annotations", annotationsToString(spaceDetails.Annotations)})
	view.Append([]string{"Organization ID", spaceDetails.OrganizationID.String()})
}

func displaySpaceDetails(spaceDetails *goclientnew.Space) {
	view := tableView()
	displaySpaceDetailsInView(spaceDetails, view)
	view.Render()
}

func totalCountMap(counts map[string]int) int {
	if len(counts) == 0 {
		return 0
	}
	total := 0
	for _, count := range counts {
		total += count
	}
	return total
}

func displayExtendedSpaceDetails(extendedSpace *goclientnew.ExtendedSpace) {
	view := tableView()
	displaySpaceDetailsInView(extendedSpace.Space, view)
	// TODO: TriggerCountByEventType, TargetCountByToolchainType
	view.Append([]string{"# Units", fmt.Sprintf("%d", extendedSpace.TotalUnitCount)})
	view.Append([]string{"# Unapplied Units", fmt.Sprintf("%d", extendedSpace.UnappliedUnitCount)})
	view.Append([]string{"# Unapproved Units", fmt.Sprintf("%d", extendedSpace.UnapprovedUnitCount)})
	view.Append([]string{"# Gated Units", fmt.Sprintf("%d", extendedSpace.GatedUnitCount)})
	view.Append([]string{"# Upgradable Units", fmt.Sprintf("%d", extendedSpace.UpgradableUnitCount)})
	view.Append([]string{"# Unlinked Units", fmt.Sprintf("%d", extendedSpace.UnlinkedUnitCount)})
	view.Append([]string{"# Recently Changed Units", fmt.Sprintf("%d", extendedSpace.RecentChangeUnitCount)})
	view.Append([]string{"# Incomplete Applies", fmt.Sprintf("%d", extendedSpace.IncompleteApplyUnitCount)})
	view.Append([]string{"# Workers", fmt.Sprintf("%d", extendedSpace.TotalBridgeWorkerCount)})
	view.Append([]string{"# Targets", fmt.Sprintf("%d", totalCountMap(extendedSpace.TargetCountByToolchainType))})
	view.Append([]string{"# Triggers", fmt.Sprintf("%d", totalCountMap(extendedSpace.TriggerCountByEventType))})
	view.Render()
}

func apiGetExtendedSpace(spaceID string) (*goclientnew.ExtendedSpace, error) {
	newParams := &goclientnew.GetSpaceParams{}
	summary := true
	newParams.Summary = &summary
	spaceRes, err := cubClientNew.GetSpaceWithResponse(ctx, uuid.MustParse(spaceID), newParams)
	if IsAPIError(err, spaceRes) {
		return nil, InterpretErrorGeneric(err, spaceRes)
	}
	return spaceRes.JSON200, nil
}

func apiGetSpace(spaceID string) (*goclientnew.Space, error) {
	newParams := &goclientnew.GetSpaceParams{}
	spaceRes, err := cubClientNew.GetSpaceWithResponse(ctx, uuid.MustParse(spaceID), newParams)
	if IsAPIError(err, spaceRes) {
		return nil, InterpretErrorGeneric(err, spaceRes)
	}
	return spaceRes.JSON200.Space, nil
}

func apiGetSpaceFromSlug(slug string) (*goclientnew.Space, error) {
	id, err := uuid.Parse(slug)
	if err == nil {
		return apiGetSpace(id.String())
	}
	spaces, err := apiListSpaces("Slug = '" + slug + "'")
	if err != nil {
		return nil, err
	}
	for _, space := range spaces {
		if space.Slug == slug {
			return space, nil
		}
	}
	return nil, fmt.Errorf("space %s not found", slug)
}
