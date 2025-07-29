// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/spf13/cobra"
)

var organizationListCmd = &cobra.Command{
	Use:   "list",
	Short: "List organizations",
	Long: `List organizations you have access to in this organization. The output includes display names, slugs, and organization IDs.

Examples:
  # List all organizations with headers
  cub organization list

  # List organizations without headers for scripting
  cub organization list --no-header

  # List organizations in JSON format
  cub organization list --json

  # List organizations with custom JQ filter
  cub organization list --jq '.[].Slug'`,
	RunE: organizationListCmdRun,
}

func init() {
	addStandardListFlags(organizationListCmd)
	organizationCmd.AddCommand(organizationListCmd)
}

func organizationListCmdRun(cmd *cobra.Command, args []string) error {
	organizations, err := apiListOrganizations(where)
	if err != nil {
		return err
	}
	displayListResults(organizations, getOrganizationSlug, displayOrganizationList)
	return nil
}

func getOrganizationSlug(organization *goclientnew.Organization) string {
	return organization.Slug
}

func displayOrganizationList(organizations []*goclientnew.Organization) {
	table := tableView()
	if !noheader {
		table.SetHeader([]string{"Display-Name", "Slug", "ID", "Billing-ID", "External-ID"})
	}
	for _, organization := range organizations {
		table.Append([]string{
			organization.DisplayName,
			organization.Slug,
			organization.OrganizationID.String(),
			organization.BillingAccountID.String(),
			organization.ExternalID,
		})
	}
	table.Render()
}

func apiListOrganizations(whereFilter string) ([]*goclientnew.Organization, error) {
	newParams := &goclientnew.ListOrganizationsParams{}
	if whereFilter != "" {
		newParams.Where = &whereFilter
	}
	orgsRes, err := cubClientNew.ListOrganizationsWithResponse(ctx, newParams)
	if IsAPIError(err, orgsRes) {
		return nil, InterpretErrorGeneric(err, orgsRes)
	}

	organizations := make([]*goclientnew.Organization, 0, len(*orgsRes.JSON200))
	for _, org := range *orgsRes.JSON200 {
		organizations = append(organizations, &org)
	}
	return organizations, nil
}
