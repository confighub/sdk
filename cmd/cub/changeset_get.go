// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"

	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var changesetGetCmd = &cobra.Command{
	Use:   "get <slug or id>",
	Short: "Get details about a changeset",
	Args:  cobra.ExactArgs(1),
	Long: `Get detailed information about a changeset in a space including its ID, slug, display name, filter, tags, and description.

Examples:
  # Get details about a release changeset
  cub changeset get --space my-space release-changeset

  # Get details about a changeset in JSON format
  cub changeset get --space my-space --json hotfix-changeset
`,
	RunE: changesetGetCmdRun,
}

func init() {
	addStandardGetFlags(changesetGetCmd)
	changesetCmd.AddCommand(changesetGetCmd)
}

func changesetGetCmdRun(cmd *cobra.Command, args []string) error {
	changesetDetails, err := apiGetChangeSetFromSlug(args[0], selectFields)
	if err != nil {
		return err
	}

	displayGetResults(changesetDetails, displayChangeSetDetails)
	return nil
}

func displayChangeSetDetails(changesetDetails *goclientnew.ChangeSet) {
	view := tableView()
	view.Append([]string{"ID", changesetDetails.ChangeSetID.String()})
	view.Append([]string{"Name", changesetDetails.Slug})
	view.Append([]string{"Display Name", changesetDetails.DisplayName})
	view.Append([]string{"Space ID", changesetDetails.SpaceID.String()})
	view.Append([]string{"Created At", changesetDetails.CreatedAt.String()})
	view.Append([]string{"Updated At", changesetDetails.UpdatedAt.String()})
	view.Append([]string{"Labels", labelsToString(changesetDetails.Labels)})
	view.Append([]string{"Annotations", annotationsToString(changesetDetails.Annotations)})
	view.Append([]string{"Organization ID", changesetDetails.OrganizationID.String()})
	
	if changesetDetails.FilterID != nil && *changesetDetails.FilterID != uuid.Nil {
		view.Append([]string{"Filter ID", changesetDetails.FilterID.String()})
	}
	if changesetDetails.StartTagID != nil && *changesetDetails.StartTagID != uuid.Nil {
		view.Append([]string{"Start Tag ID", changesetDetails.StartTagID.String()})
	}
	if changesetDetails.EndTagID != nil && *changesetDetails.EndTagID != uuid.Nil {
		view.Append([]string{"End Tag ID", changesetDetails.EndTagID.String()})
	}
	if changesetDetails.Description != "" {
		view.Append([]string{"Description", changesetDetails.Description})
	}
	
	view.Render()
}

func apiGetChangeSet(changesetID string, selectParam string) (*goclientnew.ChangeSet, error) {
	newParams := &goclientnew.GetChangeSetParams{}
	include := "FilterID,StartTagID,EndTagID"
	newParams.Include = &include
	selectValue := handleSelectParameter(selectParam, selectFields, nil)
	if selectValue != "" && selectValue != "*" {
		newParams.Select = &selectValue
	}
	changesetRes, err := cubClientNew.GetChangeSetWithResponse(ctx, uuid.MustParse(selectedSpaceID), uuid.MustParse(changesetID), newParams)
	if IsAPIError(err, changesetRes) {
		return nil, InterpretErrorGeneric(err, changesetRes)
	}
	return changesetRes.JSON200.ChangeSet, nil
}

func apiGetChangeSetFromSlug(slug string, selectParam string) (*goclientnew.ChangeSet, error) {
	id, err := uuid.Parse(slug)
	if err == nil {
		return apiGetChangeSet(id.String(), selectParam)
	}
	// The default for get is "*" rather than auto-selected list columns
	if selectParam == "" {
		selectParam = "*"
	}
	changesets, err := apiListChangeSets(selectedSpaceID, "Slug = '"+slug+"'", selectParam)
	if err != nil {
		return nil, err
	}
	// find changeset by slug
	for _, changeset := range changesets {
		if changeset.ChangeSet != nil && changeset.ChangeSet.Slug == slug {
			return changeset.ChangeSet, nil
		}
	}
	return nil, fmt.Errorf("changeset %s not found in space %s", slug, selectedSpaceSlug)
}