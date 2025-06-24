// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var targetDeleteCmd = &cobra.Command{
	Use:   "delete <slug or id>",
	Short: "Delete a target",
	Long:  `Delete a target`,
	Args:  cobra.ExactArgs(1),
	RunE:  targetDeleteCmdRun,
}

func init() {
	addStandardDeleteFlags(targetDeleteCmd)
	targetCmd.AddCommand(targetDeleteCmd)
}

func targetDeleteCmdRun(cmd *cobra.Command, args []string) error {
	targetDetails, err := apiGetTargetFromSlug(args[0], selectedSpaceID)
	if err != nil {
		return err
	}

	deleteRes, err := cubClientNew.DeleteTargetWithResponse(ctx, uuid.MustParse(selectedSpaceID), targetDetails.Target.TargetID)
	if IsAPIError(err, deleteRes) {
		return InterpretErrorGeneric(err, deleteRes)
	}
	displayDeleteResults("target", args[0], targetDetails.Target.TargetID.String())
	return nil
}
