// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

var releaseGetCmd = &cobra.Command{
	Use:   "get <release-id>",
	Short: "Get details about a release",
	Args:  cobra.ExactArgs(1),
	Long: getCommandHelp(`Get detailed information about a specific release.

Examples:
`+"```"+`
  # Get details about a release
  cub release get --space my-space 61f26b06-3c34-4363-8b9d-7d0a7c2b5f1c

  # Get a release in JSON format
  cub release get --space my-space -o json 61f26b06-3c34-4363-8b9d-7d0a7c2b5f1c
`+"```"+`
`, ""),
	RunE: releaseGetCmdRun,
}

func init() {
	addStandardGetFlags(releaseGetCmd)
	releaseCmd.AddCommand(releaseGetCmd)
}

func releaseGetCmdRun(cmd *cobra.Command, args []string) error {
	releaseID, err := uuid.Parse(args[0])
	if err != nil {
		return fmt.Errorf("invalid release id %q: %w", args[0], err)
	}

	release, err := apiGetExtendedRelease(releaseID.String(), selectFields)
	if err != nil {
		return err
	}
	displayGetResults(release, displayExtendedReleaseDetails)
	return nil
}

func displayReleaseDetailsInView(releaseDetails *goclientnew.Release, view *tablewriter.Table) {
	view.Append([]string{"ID", releaseDetails.ReleaseID.String()})
	view.Append([]string{"Digest", releaseDetails.Digest})
	view.Append([]string{"Manifest Digest", releaseDetails.ManifestDigest})
	view.Append([]string{"Organization ID", releaseDetails.OrganizationID.String()})
	view.Append([]string{"Created At", releaseDetails.CreatedAt.String()})
}

func displayReleaseDetails(releaseDetails *goclientnew.Release) {
	// Create an ExtendedRelease wrapper with just the Release set
	extendedRelease := &goclientnew.ExtendedRelease{
		Release: releaseDetails,
		// All other fields (Target, Space, Tag, etc.) will be nil, causing Extended display to show basic info
	}
	displayExtendedReleaseDetails(extendedRelease)
}

func displayExtendedReleaseDetails(er *goclientnew.ExtendedRelease) {
	view := tableView()
	if er.Release != nil {
		displayReleaseDetailsInView(er.Release, view)
	}

	// Display Target - use Slug if expanded, otherwise UUID
	if er.Target != nil {
		view.Append([]string{"Target", er.Target.Slug})
	} else if er.Release != nil && er.Release.TargetID != uuid.Nil {
		view.Append([]string{"Target ID", er.Release.TargetID.String()})
	}

	// Display Space - use Slug if expanded, otherwise UUID
	if er.Space != nil {
		view.Append([]string{"Space", er.Space.Slug})
	} else if er.Release != nil {
		view.Append([]string{"Space ID", er.Release.SpaceID.String()})
	}

	// Display Tag - use Slug if expanded, otherwise UUID
	if er.Tag != nil {
		view.Append([]string{"Tag", er.Tag.Slug})
	} else if er.Release != nil && er.Release.TagID != nil {
		view.Append([]string{"Tag ID", er.Release.TagID.String()})
	}
	view.Render()
}

func apiGetExtendedRelease(releaseID string, selectParam string) (*goclientnew.ExtendedRelease, error) {
	newParams := &goclientnew.GetExtendedReleaseParams{}
	include := "TargetID,SpaceID,TagID"
	newParams.Include = &include
	selectValue := handleSelectParameter(selectParam, selectFields, nil)
	if selectValue != "" && selectValue != "*" {
		newParams.Select = &selectValue
	}
	relRes, err := cubClientNew.GetExtendedReleaseWithResponse(ctx,
		uuid.MustParse(selectedSpaceID),
		uuid.MustParse(releaseID),
		newParams,
	)
	if cubapi.IsAPIError(err, relRes) {
		return nil, cubapi.InterpretErrorGeneric(err, relRes)
	}
	return relRes.JSON200, nil
}
