// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var bridgeworkerDeleteCmd = &cobra.Command{
	Use:   "delete <slug>",
	Short: "Delete a bridgeworker",
	Long:  `Delete a bridgeworker`,
	Args:  cobra.ExactArgs(1),
	RunE:  bridgeworkerDeleteCmdRun,
}

func init() {
	addStandardDeleteFlags(bridgeworkerDeleteCmd)
	workerCmd.AddCommand(bridgeworkerDeleteCmd)
}

func bridgeworkerDeleteCmdRun(cmd *cobra.Command, args []string) error {
	worker, err := apiGetBridgeWorkerFromSlug(args[0])
	if err != nil {
		return err
	}
	deleteRes, err := cubClientNew.DeleteBridgeWorkerWithResponse(ctx, uuid.MustParse(selectedSpaceID), worker.BridgeWorkerID)
	if IsAPIError(err, deleteRes) {
		return InterpretErrorGeneric(err, deleteRes)
	}

	displayDeleteResults("bridge_worker", args[0], worker.BridgeWorkerID.String())
	return nil
}
