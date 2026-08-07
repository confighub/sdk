// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var changeorderGetCmd = &cobra.Command{
	Use:   "get <slug or id>",
	Short: "Get details about a changeorder",
	Args:  cobra.ExactArgs(1),
	Long: getCommandHelp(`Get detailed information about a changeorder in a space including its ID, slug, display name, tags, and description.

Examples:
`+"```"+`
  # Get details about a release changeorder
  cub changeorder get --space my-space release-changeorder

  # Get details about a changeorder in JSON format
  cub changeorder get --space my-space -o json hotfix-changeorder
`+"```"+`
`, ""),
	RunE: changeorderGetCmdRun,
}

func init() {
	addStandardGetFlags(changeorderGetCmd)
	changeorderCmd.AddCommand(changeorderGetCmd)
}

func changeorderGetCmdRun(cmd *cobra.Command, args []string) error {
	changeorderDetails, err := apiGetExtendedChangeOrderFromSlug(args[0], selectFields)
	if err != nil {
		return err
	}

	displayGetResults(changeorderDetails, displayExtendedChangeOrderDetails)
	return nil
}

func displayChangeOrderDetails(changeorderDetails *goclientnew.ChangeOrder) {
	// Create an ExtendedChangeOrder wrapper with just the ChangeOrder set
	extendedChangeOrder := &goclientnew.ExtendedChangeOrder{
		ChangeOrder: changeorderDetails,
		// All other fields (Space, StartTag, EndTag, etc.) will be nil, causing Extended display to show IDs
	}
	displayExtendedChangeOrderDetails(extendedChangeOrder)
}

func displayExtendedChangeOrderDetails(extendedChangeOrder *goclientnew.ExtendedChangeOrder) {
	changeorderDetails := extendedChangeOrder.ChangeOrder
	view := tableView()
	view.Append([]string{"ID", changeorderDetails.ChangeOrderID.String()})
	view.Append([]string{"Name", changeorderDetails.Slug})

	// Show Space slug instead of Space ID when available
	if extendedChangeOrder.Space != nil {
		view.Append([]string{"Space", extendedChangeOrder.Space.Slug})
	} else {
		view.Append([]string{"Space ID", changeorderDetails.SpaceID.String()})
	}

	if changeorderDetails.UpdateType != "" {
		view.Append([]string{"Update Type", changeorderDetails.UpdateType})
	}

	view.Append([]string{"Created At", changeorderDetails.CreatedAt.String()})
	view.Append([]string{"Updated At", changeorderDetails.UpdatedAt.String()})
	view.Append([]string{"Labels", labelsToString(changeorderDetails.Labels)})
	view.Append([]string{"Delete Gates", deleteGatesToString(changeorderDetails.DeleteGates)})
	view.Append([]string{"Annotations", annotationsToString(changeorderDetails.Annotations)})
	view.Append([]string{"Organization ID", changeorderDetails.OrganizationID.String()})

	// Show related entities by slug when available
	if extendedChangeOrder.StartTag != nil {
		view.Append([]string{"Start Tag", extendedChangeOrder.StartTag.Slug})
	}
	if extendedChangeOrder.EndTag != nil {
		view.Append([]string{"End Tag", extendedChangeOrder.EndTag.Slug})
	}
	if extendedChangeOrder.SpaceFilter != nil {
		view.Append([]string{"Space Filter", extendedChangeOrder.SpaceFilter.Slug})
	}
	if changeorderDetails.Description != "" {
		view.Append([]string{"Description", changeorderDetails.Description})
	}

	view.Render()
}

func apiGetChangeOrder(changeorderID string, selectParam string) (*goclientnew.ChangeOrder, error) {
	extendedChangeOrder, err := apiGetExtendedChangeOrder(changeorderID, selectParam)
	if err != nil {
		return nil, err
	}
	return extendedChangeOrder.ChangeOrder, nil
}

func apiGetExtendedChangeOrder(changeorderID string, selectParam string) (*goclientnew.ExtendedChangeOrder, error) {
	newParams := &goclientnew.GetChangeOrderParams{}
	include := "SpaceID,StartTagID,EndTagID,SpaceFilterID"
	newParams.Include = &include
	selectValue := handleSelectParameter(selectParam, selectFields, nil)
	if selectValue != "" && selectValue != "*" {
		newParams.Select = &selectValue
	}
	changeorderRes, err := cubClientNew.GetChangeOrderWithResponse(ctx, uuid.MustParse(selectedSpaceID), uuid.MustParse(changeorderID), newParams)
	if cubapi.IsAPIError(err, changeorderRes) {
		return nil, cubapi.InterpretErrorGeneric(err, changeorderRes)
	}
	return changeorderRes.JSON200, nil
}

func apiGetChangeOrderFromSlug(slug string, selectParam string) (*goclientnew.ChangeOrder, error) {
	return apiGetChangeOrderFromSlugInSpace(slug, selectedSpaceID, selectParam)
}

func apiGetChangeOrderFromSlugWithSpace(slug string, selectParam string, spaceID string) (*goclientnew.ChangeOrder, error) {
	return apiGetChangeOrderFromSlugInSpace(slug, spaceID, selectParam)
}

func apiGetChangeOrderFromSlugInSpace(slug string, spaceID string, selectParam string) (*goclientnew.ChangeOrder, error) {
	id, err := uuid.Parse(slug)
	if err == nil {
		return apiGetChangeOrder(id.String(), selectParam)
	}
	// The default for get is "*" rather than auto-selected list columns
	if selectParam == "" {
		selectParam = "*"
	}
	changeorders, err := apiListChangeOrders(spaceID, "Slug = '"+slug+"'", selectParam, "")
	if err != nil {
		return nil, err
	}
	// find changeorder by slug
	for _, changeorder := range changeorders {
		if changeorder.ChangeOrder != nil && changeorder.ChangeOrder.Slug == slug {
			return changeorder.ChangeOrder, nil
		}
	}
	return nil, fmt.Errorf("changeorder %s not found in space %s", slug, spaceID)
}

func apiGetExtendedChangeOrderFromSlug(slug string, selectParam string) (*goclientnew.ExtendedChangeOrder, error) {
	id, err := uuid.Parse(slug)
	if err == nil {
		return apiGetExtendedChangeOrder(id.String(), selectParam)
	}
	// The default for get is "*" rather than auto-selected list columns
	if selectParam == "" {
		selectParam = "*"
	}
	changeorders, err := apiListChangeOrders(selectedSpaceID, "Slug = '"+slug+"'", selectParam, "")
	if err != nil {
		return nil, err
	}
	// find changeorder by slug
	for _, changeorder := range changeorders {
		if changeorder.ChangeOrder != nil && changeorder.ChangeOrder.Slug == slug {
			return changeorder, nil
		}
	}
	return nil, fmt.Errorf("changeorder %s not found in space %s", slug, selectedSpaceID)
}
