// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var tagGetCmd = &cobra.Command{
	Use:   "get <slug or id>",
	Short: "Get details about a tag",
	Args:  cobra.ExactArgs(1),
	Long: getCommandHelp(`Get detailed information about a tag in a space including its ID, slug, display name, and metadata.

Examples:
`+"```"+`
  # Get details about a release tag
  cub tag get --space my-space release-v1.0

  # Get details about a tag in JSON format
  cub tag get --space my-space --json production-deploy
`+"```"+`
`, ""),
	RunE: tagGetCmdRun,
}

func init() {
	addStandardGetFlags(tagGetCmd)
	enableOptionalSpace(tagGetCmd)
	tagCmd.AddCommand(tagGetCmd)
}

func tagGetCmdRun(cmd *cobra.Command, args []string) error {
	tagDetails, err := resolveTag(args[0], selectedSpaceID, selectFields)
	if err != nil {
		return err
	}

	displayGetResults(tagDetails, displayExtendedTagDetails)
	return nil
}

func displayTagDetails(tagDetails *goclientnew.Tag) {
	// Create an ExtendedTag wrapper with just the Tag set
	extendedTag := &goclientnew.ExtendedTag{
		Tag: tagDetails,
		// All other fields (Space, etc.) will be nil, causing Extended display to show IDs
	}
	displayExtendedTagDetails(extendedTag)
}

func displayExtendedTagDetails(extendedTag *goclientnew.ExtendedTag) {
	tagDetails := extendedTag.Tag
	view := tableView()
	view.Append([]string{"ID", tagDetails.TagID.String()})
	view.Append([]string{"Name", tagDetails.Slug})

	// Show Space slug instead of Space ID when available
	if extendedTag.Space != nil {
		view.Append([]string{"Space", extendedTag.Space.Slug})
	} else {
		view.Append([]string{"Space ID", tagDetails.SpaceID.String()})
	}

	// Show ChangeSet slug if available, or ChangeSetID if not nil and not uuid.Nil
	if extendedTag.ChangeSet != nil {
		view.Append([]string{"ChangeSet", extendedTag.ChangeSet.Slug})
	} else if tagDetails.ChangeSetID != nil && *tagDetails.ChangeSetID != uuid.Nil {
		view.Append([]string{"ChangeSet ID", tagDetails.ChangeSetID.String()})
	}

	view.Append([]string{"Created At", tagDetails.CreatedAt.String()})
	view.Append([]string{"Updated At", tagDetails.UpdatedAt.String()})
	view.Append([]string{"Labels", labelsToString(tagDetails.Labels)})
	view.Append([]string{"Delete Gates", deleteGatesToString(tagDetails.DeleteGates)})
	view.Append([]string{"Annotations", annotationsToString(tagDetails.Annotations)})
	view.Append([]string{"Organization ID", tagDetails.OrganizationID.String()})
	view.Render()
}
