// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"log"
	"net/url"

	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var organizationMemberListCmd = &cobra.Command{
	Use:   "list",
	Short: "List organization members",
	Long: `List organization members you have access to in this organization. The output includes Created At, User IDs, and organization IDs.

Examples:
  # List all organization-member with headers
  cub organization-member list

  # List organization-member without headers for scripting
  cub organization-member list --noheader

  # List organization-member in JSON format
  cub organization-member list --json

  # List organization-member with custom JQ filter
  cub organization-member list --jq '.[].UserID'

  # List organization-member matching a specific criteria
  cub organization-member list --where "DisplayName contains 'prod'"`,
	RunE: organizationMemberListCmdRun,
}

func init() {
	addStandardListFlags(organizationMemberListCmd)
	organizationMemberCmd.AddCommand(organizationMemberListCmd)
}

func organizationMemberListCmdRun(cmd *cobra.Command, args []string) error {
	organizationMembers, err := apiListOrganizationMembers(where)
	if err != nil {
		return err
	}
	displayListResults(organizationMembers, getSlugForOrgMember, displayOrganizationMemberList)
	return nil
}

func getSlugForOrgMember(member *goclientnew.OrganizationMember) string {
	// Return the username because get and delete expect the username
	return member.Username
}

func displayOrganizationMemberList(organizationMembers []*goclientnew.OrganizationMember) {
	table := tableView()
	if !noheader {
		table.SetHeader([]string{"User-ID", "External-ID", "Name", "Username", "Org-ID", "Org-Ext-ID"})
	}
	for _, orgMember := range organizationMembers {
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

// apiListOrganizationMembers
// TODO: where filter not implemented yet
func apiListOrganizationMembers(whereFilter string) ([]*goclientnew.OrganizationMember, error) {
	newParams := &goclientnew.ListOrganizationMembersParams{}
	if whereFilter != "" {
		whereFilter = url.QueryEscape(whereFilter)
		log.Printf("where filter: %s", whereFilter)
		newParams.Where = &whereFilter
	}
	membersRes, err := cubClientNew.ListOrganizationMembersWithResponse(ctx, uuid.MustParse(selectedOrganizationID), newParams)
	if IsAPIError(err, membersRes) {
		return nil, InterpretErrorGeneric(err, membersRes)
	}

	organizationMembers := make([]*goclientnew.OrganizationMember, 0, len(*membersRes.JSON200))
	for _, member := range *membersRes.JSON200 {
		organizationMembers = append(organizationMembers, &member)
	}
	return organizationMembers, nil
}
