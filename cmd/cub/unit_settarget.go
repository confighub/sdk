// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var unitSetTargetCmd = &cobra.Command{
	Use:   "set-target <unit-slug> <target-slug>",
	Short: "Set target for the unit",
	Long:  `Set target for the unit`,
	Args:  cobra.ExactArgs(2),
	RunE:  unitSetTargetCmdRun,
}

func init() {
	enableVerboseFlag(unitSetTargetCmd)
	enableJsonFlag(unitSetTargetCmd)
	enableJqFlag(unitSetTargetCmd)
	unitCmd.AddCommand(unitSetTargetCmd)
}

func unitSetTargetCmdRun(cmd *cobra.Command, args []string) error {
	unitSlug := args[0]
	targetSlug := args[1]
	newParams := &goclientnew.UpdateUnitParams{}
	configUnit, err := apiGetUnitFromSlug(unitSlug)
	if err != nil {
		return err
	}

	var target *goclientnew.Target
	if targetSlug == "-" {
		target = &goclientnew.Target{
			TargetID: uuid.Nil,
		}
	} else {
		exTarget, err := apiGetTargetFromSlug(targetSlug, selectedSpaceID)
		if err != nil {
			return err
		}
		target = exTarget.Target
	}
	configUnit.TargetID = &target.TargetID

	unitRes, err := cubClientNew.UpdateUnitWithResponse(ctx, uuid.MustParse(selectedSpaceID), configUnit.UnitID, newParams, *configUnit)
	if IsAPIError(err, unitRes) {
		return InterpretErrorGeneric(err, unitRes)
	}

	unitDetails := unitRes.JSON200
	tprint("Successfully set target of unit %s (%s)", args[0], unitDetails.UnitID)
	if verbose {
		displayUnitDetails(unitDetails)
	}
	if jsonOutput {
		displayJSON(unitDetails)
	}
	if jq != "" {
		displayJQ(unitDetails)
	}
	return nil
}
