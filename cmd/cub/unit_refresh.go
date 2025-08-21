// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var unitRefreshCmd = &cobra.Command{
	Use:   "refresh <unit-slug>",
	Args:  cobra.ExactArgs(1),
	Short: "Refresh a configuration unit from the target",
	Long:  "Refresh a configuration unit from the target",
	RunE:  unitRefreshCmdRun,
}

func init() {
	enableWaitFlag(unitRefreshCmd)
	enableQuietFlagForOperation(unitRefreshCmd)
	unitCmd.AddCommand(unitRefreshCmd)
}

func unitRefreshCmdRun(_ *cobra.Command, args []string) error {
	configUnit, err := apiGetUnitFromSlug(args[0], "*") // get all fields for now
	if err != nil {
		return err
	}

	refreshRes, err := cubClientNew.RefreshUnitWithResponse(ctx, uuid.MustParse(selectedSpaceID), configUnit.UnitID)
	if IsAPIError(err, refreshRes) {
		return InterpretErrorGeneric(err, refreshRes)
	}
	if wait {
		return awaitCompletion("refresh", refreshRes.JSON200)
	}

	return nil
}
