// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/spf13/cobra"
)

var organizationDeleteCmd = &cobra.Command{
	Use:   "delete <slug or id>",
	Short: "Delete a organization",
	Long: `Delete a organization.
	This is a highly restricted action authorized only to the owner of the Organization`,
	Args: cobra.ExactArgs(1),
	RunE: organizationDeleteCmdRun,
}

func init() {
	addStandardDeleteFlags(organizationDeleteCmd)
	organizationCmd.AddCommand(organizationDeleteCmd)
}

func organizationDeleteCmdRun(cmd *cobra.Command, args []string) error {
	organizationDetails, err := apiGetOrganizationFromSlug(args[0])
	if err != nil {
		return err
	}

	deleteRes, err := cubClientNew.DeleteOrganizationWithResponse(ctx, organizationDetails.OrganizationID)
	if IsAPIError(err, deleteRes) {
		return InterpretErrorGeneric(err, deleteRes)
	}
	displayDeleteResults("organization", args[0], organizationDetails.OrganizationID.String())
	return nil
}
