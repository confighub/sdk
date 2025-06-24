// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var unitDestroyCmd = &cobra.Command{
	Use:   "destroy <unit-slug>",
	Args:  cobra.ExactArgs(1),
	Short: "Destroy a configuration unit from the target",
	Long:  "Destroy a configuration unit from the target",
	RunE:  unitDestroyCmdRun,
}

func init() {
	enableWaitFlag(unitDestroyCmd)
	enableQuietFlagForOperation(unitDestroyCmd)
	unitCmd.AddCommand(unitDestroyCmd)
}

func unitDestroyCmdRun(_ *cobra.Command, args []string) error {
	configUnit, err := apiGetUnitFromSlug(args[0])
	if err != nil {
		return err
	}

	destroyRes, err := cubClientNew.DestroyUnitWithResponse(ctx, uuid.MustParse(selectedSpaceID), configUnit.UnitID)
	if IsAPIError(err, destroyRes) {
		return InterpretErrorGeneric(err, destroyRes)
	}
	if wait {
		return awaitCompletion("destroy", destroyRes.JSON200)
	}

	return nil
}
