// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var bridgeworkerUpdateCmd = &cobra.Command{
	Use:   "update <name>",
	Short: "Update a bridgeworker",
	Long:  `Update a bridgeworker.`,
	Args:  cobra.ExactArgs(1),
	RunE:  bridgeworkerUpdateCmdRun,
}

func init() {
	addStandardUpdateFlags(bridgeworkerUpdateCmd)
	workerCmd.AddCommand(bridgeworkerUpdateCmd)
}

func bridgeworkerUpdateCmdRun(cmd *cobra.Command, args []string) error {
	if err := validateStdinFlags(); err != nil {
		return err
	}
	
	currentBridgeworker, err := apiGetBridgeWorkerFromSlug(args[0])
	if err != nil {
		return err
	}
	spaceID := uuid.MustParse(selectedSpaceID)
	// Handle --from-stdin or --filename with optional --replace
	if flagPopulateModelFromStdin || flagFilename != "" {
		existingBridgeworker := currentBridgeworker
		if flagReplace {
			// Replace mode - create new entity, allow Version to be overwritten
			currentBridgeworker = new(goclientnew.BridgeWorker)
			currentBridgeworker.Version = existingBridgeworker.Version
		}
		
		if err := populateModelFromFlags(currentBridgeworker); err != nil {
			return err
		}
		
		// Ensure essential fields can't be clobbered
		currentBridgeworker.OrganizationID = existingBridgeworker.OrganizationID
		currentBridgeworker.SpaceID = existingBridgeworker.SpaceID
		currentBridgeworker.BridgeWorkerID = existingBridgeworker.BridgeWorkerID
	}
	err = setLabels(&currentBridgeworker.Labels)
	if err != nil {
		return err
	}
	// If this was set from stdin, it will be overridden
	currentBridgeworker.SpaceID = spaceID

	bridgeWorkerRes, err := cubClientNew.UpdateBridgeWorkerWithResponse(ctx, spaceID, currentBridgeworker.BridgeWorkerID, *currentBridgeworker)
	if IsAPIError(err, bridgeWorkerRes) {
		return InterpretErrorGeneric(err, bridgeWorkerRes)
	}

	bridgeworkerDetails := bridgeWorkerRes.JSON200
	displayUpdateResults(bridgeworkerDetails, "bridgeworker", args[0], bridgeworkerDetails.BridgeWorkerID.String(), displayWorkerDetails)
	return nil
}
