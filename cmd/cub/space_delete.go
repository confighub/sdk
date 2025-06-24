// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/spf13/cobra"
)

var spaceDeleteCmd = &cobra.Command{
	Use:   "delete <slug or id>",
	Short: "Delete a space",
	Long:  `Delete a space`,
	Args:  cobra.ExactArgs(1),
	RunE:  spaceDeleteCmdRun,
}

func init() {
	addStandardDeleteFlags(spaceDeleteCmd)
	spaceCmd.AddCommand(spaceDeleteCmd)
}

func spaceDeleteCmdRun(cmd *cobra.Command, args []string) error {
	spaceDetails, err := apiGetSpaceFromSlug(args[0])
	if err != nil {
		return err
	}
	spaceID := spaceDetails.SpaceID
	deleteRes, err := cubClientNew.DeleteSpaceWithResponse(ctx, spaceID)
	if IsAPIError(err, deleteRes) {
		return InterpretErrorGeneric(err, deleteRes)
	}
	displayDeleteResults("space", args[0], spaceID.String())
	return nil
}
