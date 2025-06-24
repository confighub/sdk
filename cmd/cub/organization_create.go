// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/spf13/cobra"
)

var organizationCreateCmd = &cobra.Command{
	Use:   "create <organization name>",
	Short: "Create a organization",
	Args:  cobra.ExactArgs(1),
	Long: `Create a new organization as a top-level division for your access management.

Examples:
  # Create a new organization named "my-organization" with verbose output, reading configuration from stdin
  # Verbose output prints the details of the created entity
  cub organization create --verbose --json --from-stdin my-organization

  # Create a new organization with minimal output
  cub organization create my-organization`,
	RunE: organizationCreateCmdRun,
}

func init() {
	addStandardCreateFlags(organizationCreateCmd)
	organizationCmd.AddCommand(organizationCreateCmd)
}

func organizationCreateCmdRun(cmd *cobra.Command, args []string) error {
	newBody := goclientnew.Organization{}
	if flagPopulateModelFromStdin {
		if err := populateNewModelFromStdin(newBody); err != nil {
			return err
		}
	}
	err := setLabels(&newBody.Labels)
	if err != nil {
		return err
	}

	// Even if DisplayName was set in stdin, we override it with the one from args
	newBody.DisplayName = args[0]

	// The slug cannot be set by the client. It is set from the ExternalID.

	orgRes, err := cubClientNew.CreateOrganizationWithResponse(ctx, newBody)
	if IsAPIError(err, orgRes) {
		return InterpretErrorGeneric(err, orgRes)
	}

	organizationDetails := orgRes.JSON200
	displayCreateResults(organizationDetails, "organization", args[0], organizationDetails.OrganizationID.String(), displayOrganizationDetails)
	return nil
}
