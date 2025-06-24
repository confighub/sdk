// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var unitApproveCmd = &cobra.Command{
	Use:   "approve <unit-slug>",
	Args:  cobra.ExactArgs(1),
	Short: "Approve the current configuration data of a configuration unit",
	Long:  "Approve the current configuration data of a configuration unit",
	RunE:  unitApproveCmdRun,
}

func init() {
	unitCmd.AddCommand(unitApproveCmd)
}

func unitApproveCmdRun(_ *cobra.Command, args []string) error {
	configUnit, err := apiGetUnitFromSlug(args[0])
	if err != nil {
		return err
	}

	approveRes, err := cubClientNew.ApproveUnitWithResponse(ctx, uuid.MustParse(selectedSpaceID), configUnit.UnitID)
	if IsAPIError(err, approveRes) {
		return InterpretErrorGeneric(err, approveRes)
	}

	return nil
}
