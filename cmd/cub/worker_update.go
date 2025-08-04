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
	currentBridgeworker, err := apiGetBridgeWorkerFromSlug(args[0])
	if err != nil {
		return err
	}
	spaceID := uuid.MustParse(selectedSpaceID)
	if flagPopulateModelFromStdin {
		// TODO: this could clobber a lot of fields
		if err := populateNewModelFromStdin(currentBridgeworker); err != nil {
			return err
		}
	} else if flagReplaceModelFromStdin {
		// TODO: this could clobber a lot of fields
		existingBridgeworker := currentBridgeworker
		currentBridgeworker = new(goclientnew.BridgeWorker)
		// Before reading from stdin so it can be overridden by stdin
		currentBridgeworker.Version = existingBridgeworker.Version
		if err := populateNewModelFromStdin(currentBridgeworker); err != nil {
			return err
		}
		// After reading from stdin so it can't be clobbered by stdin
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
