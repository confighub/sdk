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
	currentSpace, err := apiGetSpaceFromSlug(args[0])
	if err != nil {
		return err
	}

	newBody := currentSpace
	currentSpaceID := currentSpace.SpaceID
	if flagPopulateModelFromStdin {
		// TODO: this could clobber a lot of fields
		if err := populateNewModelFromStdin(newBody); err != nil {
			return err
		}
	} else if flagReplaceModelFromStdin {
		// TODO: this could clobber a lot of fields
		newBody = new(goclientnew.Space)
		// Before reading from stdin so it can be overridden by stdin
		newBody.Version = currentSpace.Version
		if err := populateNewModelFromStdin(newBody); err != nil {
			return err
		}
		// After reading from stdin so it can't be clobbered by stdin
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
