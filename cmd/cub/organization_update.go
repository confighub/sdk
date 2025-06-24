// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/spf13/cobra"
)

var organizationUpdateCmd = &cobra.Command{
	Use:   "update <slug or id>",
	Short: "Update a organization",
	Long:  `Update a organization.`,
	Args:  cobra.ExactArgs(1),
	RunE:  organizationUpdateCmdRun,
}

func init() {
	addStandardUpdateFlags(organizationUpdateCmd)
	organizationCmd.AddCommand(organizationUpdateCmd)
}

func organizationUpdateCmdRun(cmd *cobra.Command, args []string) error {
	currentOrganization, err := apiGetOrganizationFromSlug(args[0])
	if err != nil {
		return err
	}
	if flagPopulateModelFromStdin {
		// TODO: this could clobber a lot of fields
		if err := populateNewModelFromStdin(currentOrganization); err != nil {
			return err
		}
	} else if flagReplaceModelFromStdin {
		// TODO: this could clobber a lot of fields
		existingOrganization := currentOrganization
		currentOrganization = new(goclientnew.Organization)
		// Before reading from stdin so it can be overridden by stdin
		currentOrganization.Version = existingOrganization.Version
		if err := populateNewModelFromStdin(currentOrganization); err != nil {
			return err
		}
		// After reading from stdin so it can't be clobbered by stdin
		currentOrganization.OrganizationID = existingOrganization.OrganizationID
	}
	err = setLabels(&currentOrganization.Labels)
	if err != nil {
		return err
	}
	orgRes, err := cubClientNew.UpdateOrganizationWithResponse(ctx, currentOrganization.OrganizationID, *currentOrganization)
	if IsAPIError(err, orgRes) {
		return InterpretErrorGeneric(err, orgRes)
	}
	organizationDetails := orgRes.JSON200
	displayUpdateResults(organizationDetails, "organization", args[0], organizationDetails.OrganizationID.String(), displayOrganizationDetails)
	return nil
}
