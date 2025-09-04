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
	Long: `Get detailed information about a changeset in a space including its ID, slug, display name, tags, and description.

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
	changesetDetails, err := apiGetExtendedChangeSetFromSlug(args[0], selectFields)
	if err != nil {
		return err
	}

	displayGetResults(changesetDetails, displayExtendedChangeSetDetails)
	return nil
}

func displayChangeSetDetails(changesetDetails *goclientnew.ChangeSet) {
	// Create an ExtendedChangeSet wrapper with just the ChangeSet set
	extendedChangeSet := &goclientnew.ExtendedChangeSet{
		ChangeSet: changesetDetails,
		// All other fields (Space, StartTag, EndTag, etc.) will be nil, causing Extended display to show IDs
	}
	displayExtendedChangeSetDetails(extendedChangeSet)
}

func displayExtendedChangeSetDetails(extendedChangeSet *goclientnew.ExtendedChangeSet) {
	changesetDetails := extendedChangeSet.ChangeSet
	view := tableView()
	view.Append([]string{"ID", changesetDetails.ChangeSetID.String()})
	view.Append([]string{"Name", changesetDetails.Slug})

	// Show Space slug instead of Space ID when available
	if extendedChangeSet.Space != nil {
		view.Append([]string{"Space", extendedChangeSet.Space.Slug})
	} else {
		view.Append([]string{"Space ID", changesetDetails.SpaceID.String()})
	}

	// Show State field
	if changesetDetails.State != "" {
		view.Append([]string{"State", changesetDetails.State})
	}

	view.Append([]string{"Created At", changesetDetails.CreatedAt.String()})
	view.Append([]string{"Updated At", changesetDetails.UpdatedAt.String()})
	view.Append([]string{"Labels", labelsToString(changesetDetails.Labels)})
	view.Append([]string{"Annotations", annotationsToString(changesetDetails.Annotations)})
	view.Append([]string{"Organization ID", changesetDetails.OrganizationID.String()})

	// Show related entities by slug when available
	if extendedChangeSet.StartTag != nil {
		view.Append([]string{"Start Tag", extendedChangeSet.StartTag.Slug})
	}
	if extendedChangeSet.EndTag != nil {
		view.Append([]string{"End Tag", extendedChangeSet.EndTag.Slug})
	}
	if changesetDetails.Description != "" {
		view.Append([]string{"Description", changesetDetails.Description})
	}

	view.Render()
}

func apiGetChangeSet(changesetID string, selectParam string) (*goclientnew.ChangeSet, error) {
	extendedChangeSet, err := apiGetExtendedChangeSet(changesetID, selectParam)
	if err != nil {
		return nil, err
	}
	return extendedChangeSet.ChangeSet, nil
}

func apiGetExtendedChangeSet(changesetID string, selectParam string) (*goclientnew.ExtendedChangeSet, error) {
	newParams := &goclientnew.GetChangeSetParams{}
	include := "SpaceID,StartTagID,EndTagID"
	newParams.Include = &include
	selectValue := handleSelectParameter(selectParam, selectFields, nil)
	if selectValue != "" && selectValue != "*" {
		newParams.Select = &selectValue
	}
	changesetRes, err := cubClientNew.GetChangeSetWithResponse(ctx, uuid.MustParse(selectedSpaceID), uuid.MustParse(changesetID), newParams)
	if IsAPIError(err, changesetRes) {
		return nil, InterpretErrorGeneric(err, changesetRes)
	}
	return changesetRes.JSON200, nil
}

func apiGetChangeSetFromSlug(slug string, selectParam string) (*goclientnew.ChangeSet, error) {
	return apiGetChangeSetFromSlugInSpace(slug, selectedSpaceID, selectParam)
}

func apiGetChangeSetFromSlugWithSpace(slug string, selectParam string, spaceID string) (*goclientnew.ChangeSet, error) {
	return apiGetChangeSetFromSlugInSpace(slug, spaceID, selectParam)
}

func apiGetChangeSetFromSlugInSpace(slug string, spaceID string, selectParam string) (*goclientnew.ChangeSet, error) {
	id, err := uuid.Parse(slug)
	if err == nil {
		return apiGetChangeSet(id.String(), selectParam)
	}
	// The default for get is "*" rather than auto-selected list columns
	if selectParam == "" {
		selectParam = "*"
	}
	changesets, err := apiListChangeSets(spaceID, "Slug = '"+slug+"'", selectParam, "")
	if err != nil {
		return nil, err
	}
	// find changeset by slug
	for _, changeset := range changesets {
		if changeset.ChangeSet != nil && changeset.ChangeSet.Slug == slug {
			return changeset.ChangeSet, nil
		}
	}
	return nil, fmt.Errorf("changeset %s not found in space %s", slug, spaceID)
}

func apiGetExtendedChangeSetFromSlug(slug string, selectParam string) (*goclientnew.ExtendedChangeSet, error) {
	id, err := uuid.Parse(slug)
	if err == nil {
		return apiGetExtendedChangeSet(id.String(), selectParam)
	}
	// The default for get is "*" rather than auto-selected list columns
	if selectParam == "" {
		selectParam = "*"
	}
	changesets, err := apiListChangeSets(selectedSpaceID, "Slug = '"+slug+"'", selectParam, "")
	if err != nil {
		return nil, err
	}
	// find changeset by slug
	for _, changeset := range changesets {
		if changeset.ChangeSet != nil && changeset.ChangeSet.Slug == slug {
			return changeset, nil
		}
	}
	return nil, fmt.Errorf("changeset %s not found in space %s", slug, selectedSpaceID)
}
