// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var organizationMemberDeleteCmd = &cobra.Command{
	Use:   "delete <username or user id>",
	Short: "Delete a organization-member",
	Long:  `Delete a organization-member`,
	Args:  cobra.ExactArgs(1),
	RunE:  organizationMemberDeleteCmdRun,
}

func init() {
	addStandardDeleteFlags(organizationMemberDeleteCmd)
	organizationMemberCmd.AddCommand(organizationMemberDeleteCmd)
}

func organizationMemberDeleteCmdRun(cmd *cobra.Command, args []string) error {
	organizationMemberDetails, err := apiGetOrganizationMemberFromUsername(args[0], "*") // get all fields for now
	if err != nil {
		return err
	}
	deleteRes, err := cubClientNew.DeleteOrganizationMemberWithResponse(ctx,
		uuid.MustParse(selectedOrganizationID),
		organizationMemberDetails.UserID)
	if IsAPIError(err, deleteRes) {
		return InterpretErrorGeneric(err, deleteRes)
	}
	displayDeleteResults("organization-member", args[0], organizationMemberDetails.UserID.String())
	return nil
}
