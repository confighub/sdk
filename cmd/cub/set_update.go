// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var setUpdateCmd = &cobra.Command{
	Use:   "update <slug or id>",
	Short: "Update a set",
	Long:  `Update a set.`,
	Args:  cobra.ExactArgs(1),
	RunE:  setUpdateCmdRun,
}

func init() {
	addStandardUpdateFlags(setUpdateCmd)
	setCmd.AddCommand(setUpdateCmd)
}

func setUpdateCmdRun(cmd *cobra.Command, args []string) error {
	if err := validateStdinFlags(); err != nil {
		return err
	}

	currentSet, err := apiGetSetFromSlug(args[0], "*") // get all fields for RMW
	if err != nil {
		return err
	}

	spaceID := uuid.MustParse(selectedSpaceID)
	// Handle --from-stdin or --filename with optional --replace
	if flagPopulateModelFromStdin || flagFilename != "" {
		existingSet := currentSet
		if flagReplace {
			// Replace mode - create new entity, allow Version to be overwritten
			currentSet = new(goclientnew.Set)
			currentSet.Version = existingSet.Version
		}

		if err := populateModelFromFlags(currentSet); err != nil {
			return err
		}

		// Ensure essential fields can't be clobbered
		currentSet.OrganizationID = existingSet.OrganizationID
		currentSet.SpaceID = existingSet.SpaceID
		currentSet.SetID = existingSet.SetID
	}
	err = setLabels(&currentSet.Labels)
	if err != nil {
		return err
	}

	// If this was set from stdin, it will be overridden
	currentSet.SpaceID = spaceID

	setRes, err := cubClientNew.UpdateSetWithResponse(ctx, spaceID, currentSet.SetID, *currentSet)
	if IsAPIError(err, setRes) {
		return InterpretErrorGeneric(err, setRes)
	}
	setDetails := setRes.JSON200
	displayUpdateResults(setDetails, "set", args[0], setDetails.SetID.String(), displaySetDetails)
	return nil
}
