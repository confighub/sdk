// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"

	"github.com/confighub/sdk/cubapi"
	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var targetGetCmd = &cobra.Command{
	Use:   "get <slug or id>",
	Short: "Get details about a target",
	Args:  cobra.ExactArgs(1),
	Long: getCommandHelp(`Get detailed information about a target in a space including its ID, slug, display name, and configuration.

Examples:
`+"```"+`
  # Get details about a target
  cub target get --space my-space --json my-target
`+"```"+`
`, ""),
	RunE: targetGetCmdRun,
}

func init() {
	addStandardGetFlags(targetGetCmd)
	targetCmd.AddCommand(targetGetCmd)
}

func targetGetCmdRun(cmd *cobra.Command, args []string) error {
	targetDetails, err := apiGetTargetFromSlug(args[0], selectedSpaceID, selectFields)
	if err != nil {
		return err
	}

	displayGetResults(targetDetails, displayTargetDetails)
	return nil
}

func displayTargetDetails(extendedTarget *goclientnew.ExtendedTarget) {
	targetDetails := extendedTarget.Target
	view := tableView()
	view.Append([]string{"ID", targetDetails.TargetID.String()})
	view.Append([]string{"Name", targetDetails.Slug})

	// Show Space slug instead of Space ID when available
	if extendedTarget.Space != nil {
		view.Append([]string{"Space", extendedTarget.Space.Slug})
	} else {
		view.Append([]string{"Space ID", targetDetails.SpaceID.String()})
	}

	// Show Bridge Worker slug if available (expanded), otherwise show BridgeWorkerID
	if extendedTarget.BridgeWorker != nil {
		view.Append([]string{"Bridge Worker", extendedTarget.BridgeWorker.Slug})
	} else if targetDetails.BridgeWorkerID != uuid.Nil {
		view.Append([]string{"Bridge Worker ID", targetDetails.BridgeWorkerID.String()})
	}

	view.Append([]string{"Created At", targetDetails.CreatedAt.String()})
	view.Append([]string{"Updated At", targetDetails.UpdatedAt.String()})
	view.Append([]string{"Labels", labelsToString(targetDetails.Labels)})
	view.Append([]string{"Delete Gates", deleteGatesToString(targetDetails.DeleteGates)})
	view.Append([]string{"Annotations", annotationsToString(targetDetails.Annotations)})
	view.Append([]string{"Organization ID", targetDetails.OrganizationID.String()})
	view.Render()
}

func apiGetTarget(targetID string, selectParam string) (*goclientnew.ExtendedTarget, error) {
	newParams := &goclientnew.GetTargetParams{}
	include := "SpaceID,BridgeWorkerID"
	newParams.Include = &include
	selectValue := handleSelectParameter(selectParam, selectFields, nil)
	if selectValue != "" && selectValue != "*" {
		newParams.Select = &selectValue
	}
	targetRes, err := cubClientNew.GetTargetWithResponse(ctx, uuid.MustParse(selectedSpaceID), uuid.MustParse(targetID), newParams)
	if cubapi.IsAPIError(err, targetRes) {
		return nil, cubapi.InterpretErrorGeneric(err, targetRes)
	}
	return targetRes.JSON200, nil
}

func apiGetTargetFromSlug(slug string, spaceID string, selectParam string) (*goclientnew.ExtendedTarget, error) {
	return apiGetTargetFromSlugInSpace(slug, spaceID, selectParam)
}

// apiGetTargetFromSlugInSpaceCore returns just the Target, for use with parseEntityIdentifiers
func apiGetTargetFromSlugInSpaceCore(slug string, spaceID string, selectParam string) (*goclientnew.Target, error) {
	extendedTarget, err := apiGetTargetFromSlugInSpace(slug, spaceID, selectParam)
	if err != nil {
		return nil, err
	}
	return extendedTarget.Target, nil
}

func apiGetTargetFromSlugInSpace(slug string, spaceID string, selectParam string) (*goclientnew.ExtendedTarget, error) {
	id, err := uuid.Parse(slug)
	if err == nil {
		return apiGetTarget(id.String(), selectParam)
	}
	// The default for get is "*" rather than auto-selected list columns
	if selectParam == "" {
		selectParam = "*"
	}
	targets, err := apiListTargets(spaceID, "Slug = '"+slug+"'", selectParam, "")
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
