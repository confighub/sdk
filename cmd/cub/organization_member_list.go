// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var organizationMemberListCmd = &cobra.Command{
	Use:   "list",
	Short: "List organization members",
	Long: getCommandHelp(`List organization members you have access to in this organization. The output includes Created At, User IDs, and organization IDs.

Examples:
`+"```"+`
  # List all organization-member with headers
  cub organization-member list

  # List organization-member without headers for scripting
  cub organization-member list --no-headers

  # List organization-member in JSON format
  cub organization-member list -o json

  # List organization-member with custom JQ filter
  cub organization-member list -o jq='.[].OrganizationMember.UserID'
`+"```"+`
`, ""),
	RunE: organizationMemberListCmdRun,
}

// Default columns to display when no custom columns are specified
var defaultOrganizationMemberColumns = []string{"OrganizationMember.UserID", "OrganizationMember.ExternalID", "OrganizationMember.DisplayName", "OrganizationMember.Username", "OrganizationMember.OrganizationID", "OrganizationMember.ExternalOrganizationID"}

// OrganizationMember-specific aliases
var organizationMemberAliases = map[string]string{
	"Name": "OrganizationMember.DisplayName",
	"ID":   "OrganizationMember.UserID",
}

// OrganizationMember custom column dependencies
var organizationMemberCustomColumnDependencies = map[string][]string{}

func init() {
	addStandardListFlags(organizationMemberListCmd)
	organizationMemberCmd.AddCommand(organizationMemberListCmd)
}

func organizationMemberListCmdRun(cmd *cobra.Command, args []string) error {
	filterID, err := parseFilterFlag(filter)
	if err != nil {
		return err
	}

	// The endpoint returns every field, so there is no select to build.
	organizationMembers, err := cubapi.ListOrganizationMembers(ctx, cubClient, goclientnew.UUID(uuid.MustParse(selectedOrganizationID)),
		cubapi.NewWhere(where), cubapi.ListOpts{
			Filter:   filterID,
			Contains: contains,
		})
	if err != nil {
		return err
	}
	displayListResults(organizationMembers, getSlugForOrgMember, displayOrganizationMemberList)
	return nil
}

func getSlugForOrgMember(member *goclientnew.ExtendedOrganizationMember) string {
	// Return the username because get and delete expect the username
	return member.OrganizationMember.Username
}

func displayOrganizationMemberList(organizationMembers []*goclientnew.ExtendedOrganizationMember) {
	if displayRequestedColumns(organizationMembers, organizationMemberAliases, nil) {
		return
	}
	table := tableView()
	if !noheader {
		table.SetHeader([]string{"User-ID", "External-ID", "Name", "Username", "Org-ID", "Org-Ext-ID"})
	}
	for _, extendedOrganizationMember := range organizationMembers {
		orgMember := extendedOrganizationMember.OrganizationMember
		table.Append([]string{
			orgMember.UserID.String(),
			orgMember.ExternalID,
			orgMember.DisplayName,
			orgMember.Username,
			orgMember.OrganizationID.String(),
			orgMember.ExternalOrganizationID,
		})
	}
	table.Render()
}
