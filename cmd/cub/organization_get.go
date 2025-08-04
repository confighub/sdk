// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"

	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var organizationGetCmd = &cobra.Command{
	Use:   "get <slug or id>",
	Short: "Get details about a organization",
	Args:  cobra.ExactArgs(1),
	Long: `Get detailed information about a organization including its ID, slug, display name, and additional details.

Examples:
  # Get organization details in table format
  cub organization get my-organization

  # Get organization details in JSON format
  cub organization get --json my-organization

`,
	RunE: organizationGetCmdRun,
}

func init() {
	addStandardGetFlags(organizationGetCmd)
	organizationCmd.AddCommand(organizationGetCmd)
}

// organizationGetCmdRun is the main entry point for `cub organization get`
func organizationGetCmdRun(cmd *cobra.Command, args []string) error {
	organizationDetails, err := apiGetOrganizationFromSlug(args[0])
	if err != nil {
		return err
	}

	// the previous call got the list resource. We want the "detail" resource just in case they're different
	organizationDetails, err = apiGetOrganization(organizationDetails.OrganizationID.String())
	if err != nil {
		return err
	}
	displayGetResults(organizationDetails, displayOrganizationDetails)
	return nil
}

func displayOrganizationDetails(organizationDetails *goclientnew.Organization) {
	view := tableView()
	view.Append([]string{"Organization ID", organizationDetails.OrganizationID.String()})
	view.Append([]string{"Display Name", organizationDetails.DisplayName})
	view.Append([]string{"Created At", organizationDetails.CreatedAt.String()})
	view.Append([]string{"Updated At", organizationDetails.UpdatedAt.String()})
	view.Append([]string{"Labels", labelsToString(organizationDetails.Labels)})
	view.Append([]string{"Annotations", annotationsToString(organizationDetails.Annotations)})
	view.Append([]string{"Billing Account ID", organizationDetails.BillingAccountID.String()})
	view.Append([]string{"External ID", organizationDetails.ExternalID})
	view.Render()
}

func apiGetOrganization(organizationID string) (*goclientnew.Organization, error) {
	newParams := &goclientnew.GetOrganizationParams{}
	orgRes, err := cubClientNew.GetOrganizationWithResponse(ctx, uuid.MustParse(organizationID), newParams)
	if IsAPIError(err, orgRes) {
		return nil, InterpretErrorGeneric(err, orgRes)
	}
	return orgRes.JSON200, nil
}

func apiGetOrganizationFromSlug(slug string) (*goclientnew.Organization, error) {
	id, err := uuid.Parse(slug)
	if err == nil {
		return apiGetOrganization(id.String())
	}
	organizations, err := apiListOrganizations("Slug = '" + slug + "'")
	if err != nil {
		return nil, err
	}
	for _, organization := range organizations {
		if organization.Slug == slug {
			return organization, nil
		}
	}
	return nil, fmt.Errorf("organization %s not found", slug)
}
