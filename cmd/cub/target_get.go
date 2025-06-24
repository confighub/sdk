// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"

	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var targetGetCmd = &cobra.Command{
	Use:   "get <slug or id>",
	Short: "Get details about a target",
	Args:  cobra.ExactArgs(1),
	Long: `Get detailed information about a target in a space including its ID, slug, display name, and configuration.

Examples:
  # Get details about a target
  cub target get --space my-space --json my-target

  # Get extended information about a target
  cub target get --space my-space --json --extended my-target`,
	RunE: targetGetCmdRun,
}

func init() {
	addStandardGetFlags(targetGetCmd)
	targetCmd.AddCommand(targetGetCmd)
}

func targetGetCmdRun(cmd *cobra.Command, args []string) error {
	targetDetails, err := apiGetTargetFromSlug(args[0], selectedSpaceID)
	if err != nil {
		return err
	}
	if extended {
		targetExtended, err := apiGetTarget(targetDetails.Target.TargetID.String())
		if err != nil {
			return err
		}
		displayGetResults(targetExtended, displayTargetExtendedDetails)
		return nil
	}

	// the previous call got the list resource. We want the "detail" resource just in case they're different
	exTargetDetails, err := apiGetTarget(targetDetails.Target.TargetID.String())
	if err != nil {
		return err
	}
	displayGetResults(exTargetDetails, displayTargetDetails)
	return nil
}

func displayTargetDetails(extendedTarget *goclientnew.ExtendedTarget) {
	targetDetails := extendedTarget.Target
	view := tableView()
	view.Append([]string{"ID", targetDetails.TargetID.String()})
	view.Append([]string{"Slug", targetDetails.Slug})
	view.Append([]string{"Display Name", targetDetails.DisplayName})
	view.Append([]string{"Space ID", targetDetails.SpaceID.String()})
	view.Append([]string{"Created At", targetDetails.CreatedAt.String()})
	view.Append([]string{"Updated At", targetDetails.UpdatedAt.String()})
	view.Append([]string{"Labels", labelsToString(targetDetails.Labels)})
	view.Append([]string{"Annotations", annotationsToString(targetDetails.Annotations)})
	view.Append([]string{"Organization ID", targetDetails.OrganizationID.String()})
	view.Render()
}

func displayTargetExtendedDetails(targetExtendedDetails *goclientnew.ExtendedTarget) {
	displayTargetDetails(targetExtendedDetails)
}

func apiGetTarget(targetID string) (*goclientnew.ExtendedTarget, error) {
	newParams := &goclientnew.GetTargetParams{}
	targetRes, err := cubClientNew.GetTargetWithResponse(ctx, uuid.MustParse(selectedSpaceID), uuid.MustParse(targetID), newParams)
	if IsAPIError(err, targetRes) {
		return nil, InterpretErrorGeneric(err, targetRes)
	}
	return targetRes.JSON200, nil
}

func apiGetTargetExtended(targetID string) (*goclientnew.ExtendedTarget, error) {
	return apiGetTarget(targetID)
}

func apiGetTargetFromSlug(slug string, spaceID string) (*goclientnew.ExtendedTarget, error) {
	id, err := uuid.Parse(slug)
	if err == nil {
		return apiGetTarget(id.String())
	}
	targets, err := apiListTargets(spaceID, "Slug = '"+slug+"'")
	if err != nil {
		return nil, err
	}
	// find target by slug
	for _, target := range targets {
		if target.Target.Slug == slug {
			return target, nil
		}
	}
	return nil, fmt.Errorf("target %s not found in space %s", slug, spaceID)
}
