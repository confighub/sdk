// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var linkDeleteCmd = &cobra.Command{
	Use:   "delete <slug or id>",
	Short: "Delete a link",
	Long:  `Delete a link`,
	Args:  cobra.ExactArgs(1),
	RunE:  linkDeleteCmdRun,
}

func init() {
	addStandardDeleteFlags(linkDeleteCmd)
	enableWaitFlag(linkDeleteCmd)
	linkCmd.AddCommand(linkDeleteCmd)
}

func linkDeleteCmdRun(cmd *cobra.Command, args []string) error {
	linkDetails, err := apiGetLinkFromSlug(args[0])
	if err != nil {
		return err
	}

	linkRes, err := cubClientNew.DeleteLinkWithResponse(ctx, uuid.MustParse(selectedSpaceID), linkDetails.LinkID)
	if IsAPIError(err, linkRes) {
		return InterpretErrorGeneric(err, linkRes)
	}
	displayDeleteResults("link", args[0], linkDetails.LinkID.String())
	if wait {
		if !quiet {
			tprint("Awaiting triggers...")
		}
		fromUnitID := linkDetails.FromUnitID
		unitDetails, err := apiGetUnit(fromUnitID.String())
		if err != nil {
			return err
		}
		err = awaitTriggersRemoval(unitDetails)
		if err != nil {
			return err
		}
	}
	return nil
}
