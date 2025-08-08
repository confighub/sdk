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
	if err := validateStdinFlags(); err != nil {
		return err
	}
	
	currentOrganization, err := apiGetOrganizationFromSlug(args[0])
	if err != nil {
		return err
	}
	// Handle --from-stdin or --filename with optional --replace
	if flagPopulateModelFromStdin || flagFilename != "" {
		existingOrganization := currentOrganization
		if flagReplace {
			// Replace mode - create new entity, allow Version to be overwritten
			currentOrganization = new(goclientnew.Organization)
			currentOrganization.Version = existingOrganization.Version
		}
		
		if err := populateModelFromFlags(currentOrganization); err != nil {
			return err
		}
		
		// Ensure essential fields can't be clobbered
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
