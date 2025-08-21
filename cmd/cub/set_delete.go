// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var setDeleteCmd = &cobra.Command{
	Use:   "delete <slug or id>",
	Short: "Delete a set",
	Long:  `Delete a set`,
	Args:  cobra.ExactArgs(1),
	RunE:  setDeleteCmdRun,
}

func init() {
	addStandardDeleteFlags(setDeleteCmd)
	setCmd.AddCommand(setDeleteCmd)
}

func setDeleteCmdRun(cmd *cobra.Command, args []string) error {
	setDetails, err := apiGetSetFromSlug(args[0], "*") // get all fields for now
	if err != nil {
		return err
	}
	deleteRes, err := cubClientNew.DeleteSetWithResponse(ctx, uuid.MustParse(selectedSpaceID), setDetails.SetID)
	if IsAPIError(err, deleteRes) {
		return InterpretErrorGeneric(err, deleteRes)
	}
	displayDeleteResults("set", args[0], setDetails.SetID.String())
	return nil
}
