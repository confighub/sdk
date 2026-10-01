// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/spf13/cobra"
)

var organizationListCmd = &cobra.Command{
	Use:   "list",
	Short: "List organizations",
	Long: getCommandHelp(`List organizations you have access to in this organization. The output includes display names, slugs, and organization IDs.

Examples:
`+"```"+`
  # List all organizations with headers
  cub organization list

  # List organizations without headers for scripting
  cub organization list --no-headers

  # List organizations in JSON format
  cub organization list -o json

  # List organizations with custom JQ filter
  cub organization list -o jq='.[].Organization.Slug'
`+"```"+`
`, ""),
	RunE: organizationListCmdRun,
}

// Default columns to display when no custom columns are specified
var defaultOrganizationColumns = []string{"Organization.DisplayName", "Organization.OrganizationID", "Organization.ExternalID"}

// organizationBaseSelectFields are the fields always returned by organization list queries.
var organizationBaseSelectFields = []string{"Slug", "OrganizationID"}

// Organization-specific aliases
var organizationAliases = map[string]string{
	"Name": "Organization.DisplayName",
	"ID":   "Organization.OrganizationID",
}

// Organization custom column dependencies
var organizationCustomColumnDependencies = map[string][]string{}

func init() {
	addStandardListFlags(organizationListCmd)
	organizationCmd.AddCommand(organizationListCmd)
}

func organizationListCmdRun(cmd *cobra.Command, args []string) error {
	filterID, err := parseFilterFlag(filter)
	if err != nil {
		return err
	}

	selectValue := handleSelectParameter(selectFields, selectFields, func() string {
		return buildSelectList("Organization", listColumnsFor("cub organization list"), "", defaultOrganizationColumns, organizationAliases, organizationCustomColumnDependencies, organizationBaseSelectFields)
	})
	organizations, err := cubapi.ListOrganizations(ctx, cubClient, cubapi.NewWhere(where), cubapi.ListOpts{
		Select:        cubapi.SelectFields(selectValue),
		Filter:        filterID,
		Contains:      contains,
		IncludeHidden: includeHidden,
	})
	if err != nil {
		return err
	}
	displayListResults(organizations, getOrganizationSlug, displayOrganizationList)
	return nil
}

func getOrganizationSlug(organization *goclientnew.ExtendedOrganization) string {
	return organization.Organization.Slug
}

func displayOrganizationList(organizations []*goclientnew.ExtendedOrganization) {
	if displayRequestedColumns(organizations, organizationAliases, nil) {
		return
	}
	table := tableView()
	if !noheader {
		table.SetHeader([]string{"Display-Name", "ID", "External-ID"})
	}
	for _, extendedOrganization := range organizations {
		organization := extendedOrganization.Organization
		table.Append([]string{
			organization.DisplayName,
			organization.OrganizationID.String(),
			organization.ExternalID,
		})
	}
	table.Render()
}
