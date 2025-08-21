// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"

	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var tagGetCmd = &cobra.Command{
	Use:   "get <slug or id>",
	Short: "Get details about a tag",
	Args:  cobra.ExactArgs(1),
	Long: `Get detailed information about a tag in a space including its ID, slug, display name, and metadata.

Examples:
  # Get details about a release tag
  cub tag get --space my-space release-v1.0

  # Get details about a tag in JSON format
  cub tag get --space my-space --json production-deploy
`,
	RunE: tagGetCmdRun,
}

func init() {
	addStandardGetFlags(tagGetCmd)
	tagCmd.AddCommand(tagGetCmd)
}

func tagGetCmdRun(cmd *cobra.Command, args []string) error {
	tagDetails, err := apiGetTagFromSlug(args[0], selectFields)
	if err != nil {
		return err
	}

	displayGetResults(tagDetails, displayTagDetails)
	return nil
}

func displayTagDetails(tagDetails *goclientnew.Tag) {
	view := tableView()
	view.Append([]string{"ID", tagDetails.TagID.String()})
	view.Append([]string{"Name", tagDetails.Slug})
	view.Append([]string{"Display Name", tagDetails.DisplayName})
	view.Append([]string{"Space ID", tagDetails.SpaceID.String()})
	view.Append([]string{"Created At", tagDetails.CreatedAt.String()})
	view.Append([]string{"Updated At", tagDetails.UpdatedAt.String()})
	view.Append([]string{"Labels", labelsToString(tagDetails.Labels)})
	view.Append([]string{"Annotations", annotationsToString(tagDetails.Annotations)})
	view.Append([]string{"Organization ID", tagDetails.OrganizationID.String()})
	view.Render()
}

func apiGetTag(tagID string, selectParam string) (*goclientnew.Tag, error) {
	newParams := &goclientnew.GetTagParams{}
	selectValue := handleSelectParameter(selectParam, selectFields, nil)
	if selectValue != "" && selectValue != "*" {
		newParams.Select = &selectValue
	}
	tagRes, err := cubClientNew.GetTagWithResponse(ctx, uuid.MustParse(selectedSpaceID), uuid.MustParse(tagID), newParams)
	if IsAPIError(err, tagRes) {
		return nil, InterpretErrorGeneric(err, tagRes)
	}
	return tagRes.JSON200.Tag, nil
}

func apiGetTagFromSlug(slug string, selectParam string) (*goclientnew.Tag, error) {
	id, err := uuid.Parse(slug)
	if err == nil {
		return apiGetTag(id.String(), selectParam)
	}
	// The default for get is "*" rather than auto-selected list columns
	if selectParam == "" {
		selectParam = "*"
	}
	tags, err := apiListTags(selectedSpaceID, "Slug = '"+slug+"'", selectParam)
	if err != nil {
		return nil, err
	}
	// find tag by slug
	for _, tag := range tags {
		if tag.Tag != nil && tag.Tag.Slug == slug {
			return tag.Tag, nil
		}
	}
	return nil, fmt.Errorf("tag %s not found in space %s", slug, selectedSpaceSlug)
}