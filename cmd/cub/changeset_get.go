// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/spf13/cobra"
)

var changesetGetCmd = &cobra.Command{
	Use:   "get <slug or id>",
	Short: "Get details about a changeset",
	Args:  cobra.ExactArgs(1),
	Long: getCommandHelp(`Get detailed information about a changeset in a space including its ID, slug, display name, tags, and description.

Examples:
`+"```"+`
  # Get details about a release changeset
  cub changeset get --space my-space release-changeset

  # Get details about a changeset in JSON format
  cub changeset get --space my-space -o json hotfix-changeset
`+"```"+`
`, ""),
	RunE: changesetGetCmdRun,
}

func init() {
	addStandardGetFlags(changesetGetCmd)
	enableOptionalSpace(changesetGetCmd)
	changesetCmd.AddCommand(changesetGetCmd)
}

func changesetGetCmdRun(cmd *cobra.Command, args []string) error {
	changesetDetails, err := resolveChangeSet(args[0], selectedSpaceID, selectFields)
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
	view.Append([]string{"Delete Gates", deleteGatesToString(changesetDetails.DeleteGates)})
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
