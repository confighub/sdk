// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"

	"github.com/confighub/sdk/cubapi"
	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

var spaceGetCmd = &cobra.Command{
	Use:   "get <name or id>",
	Short: "Get details about a space",
	Args:  cobra.ExactArgs(1),
	Long: getCommandHelp(`Get detailed information about a space including its ID, name, and organization details.

Examples:
`+"```"+`
  # Get space details in table format
  cub space get my-space

  # Get space details in JSON format
  cub space get --json my-space
`+"```"+`
`, ""),
	RunE: spaceGetCmdRun,
}

func init() {
	addStandardGetFlags(spaceGetCmd)
	spaceCmd.AddCommand(spaceGetCmd)
}

func spaceGetCmdRun(cmd *cobra.Command, args []string) error {
	extendedSpace, err := apiGetExtendedSpaceFromSlug(args[0], selectFields)
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
	view.Append([]string{"Delete Gates", deleteGatesToString(spaceDetails.DeleteGates)})
	view.Append([]string{"Annotations", annotationsToString(spaceDetails.Annotations)})
	view.Append([]string{"Where Trigger", spaceDetails.WhereTrigger})
	view.Append([]string{"Organization ID", spaceDetails.OrganizationID.String()})
}

func displaySpaceDetails(spaceDetails *goclientnew.Space) {
	// Create an ExtendedSpace wrapper with just the Space set
	extendedSpace := &goclientnew.ExtendedSpace{
		Space: spaceDetails,
		// All other fields (Organization, counts, etc.) will be nil/zero, causing Extended display to show basic info
	}
	displayExtendedSpaceDetails(extendedSpace)
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
	view.Append([]string{"# Incomplete Applies", fmt.Sprintf("%d", extendedSpace.IncompleteApplyUnitCount)})
	view.Append([]string{"# Workers", fmt.Sprintf("%d", extendedSpace.TotalBridgeWorkerCount)})
	view.Append([]string{"# Filters", fmt.Sprintf("%d", extendedSpace.TotalFilterCount)})
	view.Append([]string{"# Views", fmt.Sprintf("%d", extendedSpace.TotalViewCount)})
	view.Append([]string{"# Tags", fmt.Sprintf("%d", extendedSpace.TotalTagCount)})
	view.Append([]string{"# ChangeSets", fmt.Sprintf("%d", extendedSpace.TotalChangeSetCount)})
	view.Append([]string{"# Invocations", fmt.Sprintf("%d", extendedSpace.TotalInvocationCount)})
	view.Append([]string{"# Targets", fmt.Sprintf("%d", totalCountMap(extendedSpace.TargetCountByToolchainType))})
	view.Append([]string{"# Triggers", fmt.Sprintf("%d", totalCountMap(extendedSpace.TriggerCountByEventType))})
	view.Render()
}

func apiGetExtendedSpace(spaceID string, selectParam string) (*goclientnew.ExtendedSpace, error) {
	newParams := &goclientnew.GetSpaceParams{}
	summary := true
	newParams.Summary = &summary
	selectValue := handleSelectParameter(selectParam, selectFields, nil)
	if selectValue != "" && selectValue != "*" {
		newParams.Select = &selectValue
	}
	spaceRes, err := cubClientNew.GetSpaceWithResponse(ctx, uuid.MustParse(spaceID), newParams)
	if cubapi.IsAPIError(err, spaceRes) {
		return nil, cubapi.InterpretErrorGeneric(err, spaceRes)
	}
	return spaceRes.JSON200, nil
}

func apiGetSpace(spaceID string, selectParam string) (*goclientnew.Space, error) {
	newParams := &goclientnew.GetSpaceParams{}
	selectValue := handleSelectParameter(selectParam, selectFields, nil)
	if selectValue != "" && selectValue != "*" {
		newParams.Select = &selectValue
	}
	spaceRes, err := cubClientNew.GetSpaceWithResponse(ctx, uuid.MustParse(spaceID), newParams)
	if cubapi.IsAPIError(err, spaceRes) {
		return nil, cubapi.InterpretErrorGeneric(err, spaceRes)
	}
	return spaceRes.JSON200.Space, nil
}

func apiGetSpaceFromSlug(slug string, selectParam string) (*goclientnew.Space, error) {
	id, err := uuid.Parse(slug)
	if err == nil {
		return apiGetSpace(id.String(), selectParam)
	}
	// The default for get is "*" rather than auto-selected list columns
	if selectParam == "" {
		selectParam = "*"
	}
	spaces, err := apiListSpaces("Slug = '"+slug+"'", selectParam)
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

func apiGetExtendedSpaceFromSlug(slug string, selectParam string) (*goclientnew.ExtendedSpace, error) {
	id, err := uuid.Parse(slug)
	if err == nil {
		return apiGetExtendedSpace(id.String(), selectParam)
	}
	// The default for get is "*" rather than auto-selected list columns
	if selectParam == "" {
		selectParam = "*"
	}
	spaces, err := apiListSpaces("Slug = '"+slug+"'", selectParam)
	if err != nil {
		return nil, err
	}
	for _, space := range spaces {
		if space.Slug == slug {
			return apiGetExtendedSpace(space.SpaceID.String(), selectParam)
		}
	}
	return nil, fmt.Errorf("space %s not found", slug)
}
