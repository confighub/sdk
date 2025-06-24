// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var targetUpdateCmd = &cobra.Command{
	Use:   "update <slug or id>",
	Short: "Update a target",
	Long:  `Update a target.`,
	Args:  cobra.ExactArgs(1),
	RunE:  targetUpdateCmdRun,
}

func init() {
	addStandardUpdateFlags(targetUpdateCmd)
	targetCmd.AddCommand(targetUpdateCmd)
}

func targetUpdateCmdRun(cmd *cobra.Command, args []string) error {
	currentTarget, err := apiGetTargetFromSlug(args[0], selectedSpaceID)
	if err != nil {
		return err
	}

	spaceID := uuid.MustParse(selectedSpaceID)
	if flagPopulateModelFromStdin {
		if err := populateNewModelFromStdin(currentTarget.Target); err != nil {
			return err
		}
	} else if flagReplaceModelFromStdin {
		// TODO: this could clobber a lot of fields
		existingTarget := currentTarget.Target
		currentTarget.Target = new(goclientnew.Target)
		// Before reading from stdin so it can be overridden by stdin
		currentTarget.Target.Version = existingTarget.Version
		if err := populateNewModelFromStdin(currentTarget.Target); err != nil {
			return err
		}
		// After reading from stdin so it can't be clobbered by stdin
		currentTarget.Target.OrganizationID = existingTarget.OrganizationID
		currentTarget.Target.SpaceID = existingTarget.SpaceID
		currentTarget.Target.TargetID = existingTarget.TargetID
	}

	err = validateToolchainAndProvider(currentTarget.Target.ToolchainType, currentTarget.Target.ProviderType)
	if err != nil {
		return err
	}

	err = setLabels(&currentTarget.Target.Labels)
	if err != nil {
		return err
	}
	// If this was set from stdin, it will be overridden
	currentTarget.Target.SpaceID = spaceID

	targetRes, err := cubClientNew.UpdateTargetWithResponse(ctx, spaceID, currentTarget.Target.TargetID, *currentTarget.Target)
	if IsAPIError(err, targetRes) {
		return InterpretErrorGeneric(err, targetRes)
	}

	targetDetails := targetRes.JSON200
	extendedDetails := &goclientnew.ExtendedTarget{Target: targetDetails}
	displayUpdateResults(extendedDetails, "target", args[0], targetDetails.TargetID.String(), displayTargetDetails)
	return nil
}
