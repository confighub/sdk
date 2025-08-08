// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/spf13/cobra"
)

var spaceUpdateCmd = &cobra.Command{
	Use:   "update <slug or id>",
	Short: "Update a space",
	Long:  `Update a space.`,
	Args:  cobra.ExactArgs(1),
	RunE:  spaceUpdateCmdRun,
}

func init() {
	addStandardUpdateFlags(spaceUpdateCmd)
	spaceCmd.AddCommand(spaceUpdateCmd)
}

func spaceUpdateCmdRun(cmd *cobra.Command, args []string) error {
	if err := validateStdinFlags(); err != nil {
		return err
	}

	currentSpace, err := apiGetSpaceFromSlug(args[0])
	if err != nil {
		return err
	}

	newBody := currentSpace
	currentSpaceID := currentSpace.SpaceID
	
	// Handle --from-stdin or --filename with optional --replace
	if flagPopulateModelFromStdin || flagFilename != "" {
		if flagReplace {
			// Replace mode - create new entity, allow Version to be overwritten
			newBody = new(goclientnew.Space)
			newBody.Version = currentSpace.Version
		}
		
		if err := populateModelFromFlags(newBody); err != nil {
			return err
		}
		
		// Ensure essential fields can't be clobbered
		newBody.OrganizationID = currentSpace.OrganizationID
		newBody.SpaceID = currentSpace.SpaceID
	}
	err = setLabels(&newBody.Labels)
	if err != nil {
		return err
	}

	spaceRes, err := cubClientNew.UpdateSpaceWithResponse(ctx, currentSpaceID, *newBody)
	if IsAPIError(err, spaceRes) {
		return InterpretErrorGeneric(err, spaceRes)
	}

	spaceDetails := spaceRes.JSON200
	displayUpdateResults(spaceDetails, "space", args[0], spaceDetails.SpaceID.String(), displaySpaceDetails)
	return nil
}
