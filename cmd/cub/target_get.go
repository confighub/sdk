// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"time"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/spf13/cobra"
)

var targetGetCmd = &cobra.Command{
	Use:   "get <slug or id>",
	Short: "Get details about a target",
	Args:  cobra.ExactArgs(1),
	Long: getCommandHelp(`Get detailed information about a target in a space including its ID, slug, and configuration.

Examples:
`+"```"+`
  # Get details about a target
  cub target get --space my-space --json my-target

  # Wait for target to be created (e.g., by a worker registering)
  cub target get --space my-space --wait my-target
`+"```"+`
`, ""),
	RunE: targetGetCmdRun,
}

func init() {
	addStandardGetFlags(targetGetCmd)
	enableOptionalSpace(targetGetCmd)
	enableGetWaitFlag(targetGetCmd)
	targetCmd.AddCommand(targetGetCmd)
}

func targetGetCmdRun(cmd *cobra.Command, args []string) error {
	if getWait {
		return targetGetWait(args[0])
	}

	targetDetails, err := resolveTarget(args[0], selectedSpaceID, "")
	if err != nil {
		return err
	}

	displayGetResults(targetDetails, displayTargetDetails)
	return nil
}

func targetGetWait(slug string) error {
	timeoutDuration := DefaultCreationTimeoutDuration
	if timeout != "" {
		d, err := time.ParseDuration(timeout)
		if err != nil {
			return fmt.Errorf("invalid timeout: %w", err)
		}
		timeoutDuration = d
	}

	deadline := time.Now().Add(timeoutDuration)
	interval := 2 * time.Second

	for {
		targetDetails, err := resolveTarget(slug, selectedSpaceID, "")
		if err == nil {
			displayGetResults(targetDetails, displayTargetDetails)
			return nil
		}
		if !cubapi.IsNotFoundError(err) {
			return err
		}
		if time.Now().Add(interval).After(deadline) {
			return fmt.Errorf("timed out waiting for target %s to be created", slug)
		}
		time.Sleep(interval)
	}
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

	view.Append([]string{"Created At", targetDetails.CreatedAt.String()})
	view.Append([]string{"Updated At", targetDetails.UpdatedAt.String()})
	view.Append([]string{"Labels", labelsToString(targetDetails.Labels)})
	appendBackingUnitRow(view, targetDetails.BackingUnitID)
	view.Append([]string{"Annotations", annotationsToString(targetDetails.Annotations)})
	view.Append([]string{"Facts", labelsToString(targetDetails.Facts)})
	view.Append([]string{"Delete Gates", deleteGatesToString(targetDetails.DeleteGates)})
	view.Append([]string{"Permissions", permissionsToString(targetDetails.Permissions)})

	view.Append([]string{"Where Trigger", targetDetails.WhereTrigger})

	// Display TriggerFilter - use Slug if expanded, otherwise UUID
	if extendedTarget.TriggerFilter != nil {
		view.Append([]string{"Trigger Filter", extendedTarget.TriggerFilter.Slug})
	} else if targetDetails.TriggerFilterID != nil {
		view.Append([]string{"Trigger Filter", targetDetails.TriggerFilterID.String()})
	} else {
		view.Append([]string{"Trigger Filter", ""})
	}

	// Display Triggers - use Slugs if expanded, otherwise UUIDs
	if len(extendedTarget.Triggers) > 0 {
		view.Append([]string{"Triggers", triggerSliceSlugsToString(extendedTarget.Triggers)})
	} else if len(targetDetails.TriggerIDs) > 0 {
		view.Append([]string{"Triggers", uuidSliceToString(targetDetails.TriggerIDs)})
	} else {
		view.Append([]string{"Triggers", ""})
	}
	view.Append([]string{"Trigger Hash", targetDetails.TriggerHash})

	view.Append([]string{"Organization ID", targetDetails.OrganizationID.String()})
	view.Render()
}
