// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var unitDeleteCmd = &cobra.Command{
	Use:   "delete <slug or id>",
	Short: "Delete a unit",
	Long:  `Delete a unit`,
	Args:  cobra.ExactArgs(1),
	RunE:  unitDeleteCmdRun,
}

func init() {
	addStandardDeleteFlags(unitDeleteCmd)
	unitCmd.AddCommand(unitDeleteCmd)
}

func unitDeleteCmdRun(cmd *cobra.Command, args []string) error {
	unitDetails, err := apiGetUnitFromSlug(args[0])
	if err != nil {
		return err
	}
	deleteRes, err := cubClientNew.DeleteUnitWithResponse(ctx, uuid.MustParse(selectedSpaceID), unitDetails.UnitID)
	if IsAPIError(err, deleteRes) {
		return InterpretErrorGeneric(err, deleteRes)
	}

	displayDeleteResults("unit", args[0], unitDetails.UnitID.String())
	return nil
}
