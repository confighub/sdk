// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/confighub/sdk/core/cubapi"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var organizationMemberDeleteCmd = &cobra.Command{
	Use:   "delete <username or user id>",
	Short: "Delete a organization-member",
	Long:  getCommandHelp(`Delete a organization-member`, ""),
	Args:  cobra.ExactArgs(1),
	RunE:  organizationMemberDeleteCmdRun,
}

func init() {
	addStandardDeleteFlags(organizationMemberDeleteCmd)
	organizationMemberCmd.AddCommand(organizationMemberDeleteCmd)
}

func organizationMemberDeleteCmdRun(cmd *cobra.Command, args []string) error {
	extendedOrganizationMember, err := resolveOrganizationMember(args[0])
	if err != nil {
		return err
	}
	organizationMemberDetails := extendedOrganizationMember.OrganizationMember
	deleteRes, err := cubClientNew.DeleteOrganizationMemberWithResponse(ctx,
		uuid.MustParse(selectedOrganizationID),
		organizationMemberDetails.UserID)
	if cubapi.IsAPIError(err, deleteRes) {
		return cubapi.InterpretErrorGeneric(err, deleteRes)
	}
	displayDeleteResults("organization-member", args[0], organizationMemberDetails.UserID.String(), deleteRes)
	return nil
}
