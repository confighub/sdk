// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/spf13/cobra"
)

var organizationGetCmd = &cobra.Command{
	Use:   "get <slug or id>",
	Short: "Get details about a organization",
	Args:  cobra.ExactArgs(1),
	Long: getCommandHelp(`Get detailed information about a organization including its ID, slug, display name, and additional details.

Examples:
`+"```"+`
  # Get organization details in table format
  cub organization get my-organization

  # Get organization details in JSON format
  cub organization get --json my-organization

  # Get the organization's ID
  cub organization get my-organization -o jq=.Organization.OrganizationID
`+"```"+`
`, ""),
	RunE: organizationGetCmdRun,
}

func init() {
	addStandardGetFlags(organizationGetCmd)
	organizationCmd.AddCommand(organizationGetCmd)
}

// organizationGetCmdRun is the main entry point for `cub organization get`
func organizationGetCmdRun(cmd *cobra.Command, args []string) error {
	extendedOrganization, err := resolveOrganization(args[0], selectFields)
	if err != nil {
		return err
	}

	displayGetResults(extendedOrganization, displayExtendedOrganizationDetails)
	return nil
}

// displayExtendedOrganizationDetails renders what get returns: the Organization wrapped the way
// every other get's entity is, so -o json and -o jq read it as .Organization, as list does.
func displayExtendedOrganizationDetails(extendedOrganization *goclientnew.ExtendedOrganization) {
	displayOrganizationDetails(extendedOrganization.Organization)
}

func displayOrganizationDetails(organizationDetails *goclientnew.Organization) {
	view := tableView()
	view.Append([]string{"Organization ID", organizationDetails.OrganizationID.String()})
	view.Append([]string{"Display Name", organizationDetails.DisplayName})
	view.Append([]string{"Created At", organizationDetails.CreatedAt.String()})
	view.Append([]string{"Updated At", organizationDetails.UpdatedAt.String()})
	view.Append([]string{"Labels", labelsToString(organizationDetails.Labels)})
	view.Append([]string{"Delete Gates", deleteGatesToString(organizationDetails.DeleteGates)})
	view.Append([]string{"Annotations", annotationsToString(organizationDetails.Annotations)})
	view.Append([]string{"External ID", organizationDetails.ExternalID})
	view.Append([]string{"Email Domain", organizationDetails.EmailDomain})
	view.Render()
}

// apiGetOrganizationFromExternalID finds an organization by the ID the identity provider knows
// it by, which is how a context names its organization.
func apiGetOrganizationFromExternalID(extID string) (*goclientnew.Organization, error) {
	organizations, err := cubapi.ListOrganizations(ctx, cubClient, cubapi.Where{}.Eq("ExternalID", extID), cubapi.ListOpts{})
	if err != nil {
		return nil, err
	}
	for _, organization := range organizations {
		if organization.Organization != nil && organization.Organization.ExternalID == extID {
			return organization.Organization, nil
		}
	}
	return nil, fmt.Errorf("organization %s not found", extID)
}
