// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var triggerDeleteCmd = &cobra.Command{
	Use:   "delete <slug or id>",
	Short: "Delete a trigger",
	Long:  `Delete a trigger`,
	Args:  cobra.ExactArgs(1),
	RunE:  triggerDeleteCmdRun,
}

func init() {
	addStandardDeleteFlags(triggerDeleteCmd)
	triggerCmd.AddCommand(triggerDeleteCmd)
}

func triggerDeleteCmdRun(cmd *cobra.Command, args []string) error {
	triggerDetails, err := apiGetTriggerFromSlug(args[0])
	if err != nil {
		return err
	}
	deleteRes, err := cubClientNew.DeleteTriggerWithResponse(ctx, uuid.MustParse(selectedSpaceID), triggerDetails.TriggerID)
	if IsAPIError(err, deleteRes) {
		return InterpretErrorGeneric(err, deleteRes)
	}

	displayDeleteResults("trigger", args[0], triggerDetails.TriggerID.String())
	return nil
}
