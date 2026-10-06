// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/spf13/cobra"
)

var organizationCreateCmd = &cobra.Command{
	Use:   "create <organization name> --email-domain <domain>",
	Short: "Create an organization",
	Args:  cobra.ExactArgs(1),
	Long: getCommandHelp(`Create a new organization as a top-level division for your access management.

The identity provider requires an email domain for each organization: give it with
--email-domain, or as EmailDomain in the configuration read with --from-stdin or --filename.

Examples:
`+"```"+`
  # Create a new organization named "my-organization" with verbose output, reading configuration from stdin
  # Verbose output prints the details of the created entity
  cub organization create --verbose --json --from-stdin my-organization

  # Create a new organization with minimal output
  cub organization create my-organization --email-domain example.com
`+"```"+`
`, ""),
	RunE: organizationCreateCmdRun,
}

var organizationCreateEmailDomain string

func init() {
	addCreateFlagsWithoutDryRun(organizationCreateCmd)
	organizationCreateCmd.Flags().StringVar(&organizationCreateEmailDomain, "email-domain", "",
		"email domain of the organization, which the identity provider requires; overrides EmailDomain read with --from-stdin or --filename")
	organizationCmd.AddCommand(organizationCreateCmd)
}

func organizationCreateCmdRun(cmd *cobra.Command, args []string) error {
	if err := validateStdinFlags(); err != nil {
		return err
	}

	newBody := goclientnew.Organization{}
	if flagPopulateModelFromStdin || flagFilename != "" {
		if err := populateModelFromFlags(&newBody); err != nil {
			return err
		}
	}

	// Validate no label removal
	if err := ValidateLabelRemoval(label, false); err != nil {
		return err
	}
	// Validate no delete gate removal
	if err := ValidateDeleteGateRemoval(deleteGate, false); err != nil {
		return err
	}

	err := setAnnotations(&newBody.Annotations)
	if err != nil {
		return err
	}
	err = setLabels(&newBody.Labels)
	if err != nil {
		return err
	}
	err = setDeleteGates(&newBody.DeleteGates)
	if err != nil {
		return err
	}

	// Even if DisplayName was set in stdin, we override it with the one from args
	newBody.DisplayName = args[0]
	if organizationCreateEmailDomain != "" {
		newBody.EmailDomain = organizationCreateEmailDomain
	}

	// The slug cannot be set by the client. It is set from the ExternalID.

	// Create params with AllowExists if needed
	params := &goclientnew.CreateOrganizationParams{}
	if allowExists {
		allowExistsStr := "true"
		params.AllowExists = &allowExistsStr
	}

	orgRes, err := cubClientNew.CreateOrganizationWithResponse(ctx, params, newBody)
	if cubapi.IsAPIError(err, orgRes) {
		return cubapi.InterpretErrorGeneric(err, orgRes)
	}

	organizationDetails := orgRes.JSON200
	displayCreateResults(organizationDetails, "organization", args[0], organizationDetails.OrganizationID.String(), displayOrganizationDetails)
	return nil
}
