// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"

	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var organizationMemberGetCmd = &cobra.Command{
	Use:   "get <organization-member>",
	Short: "Get details about a organization-member",
	Args:  cobra.ExactArgs(1),
	Long: `Get detailed information about a organization-member in an organization including its ID, User ID, and Organization ID.

Examples:
  # Get details about a organization-member
  cub organization-member get --json my-organization-member

`,
	RunE: organizationMemberGetCmdRun,
}

func init() {
	addStandardGetFlags(organizationMemberGetCmd)
	organizationMemberCmd.AddCommand(organizationMemberGetCmd)
}

func organizationMemberGetCmdRun(cmd *cobra.Command, args []string) error {
	organizationMemberDetails, err := apiGetOrganizationMemberFromUsername(args[0])
	if err != nil {
		return err
	}

	// the previous call got the list resource. We want the "detail" resource just in case they're different
	exOrganizationMemberDetails, err := apiGetOrganizationMember(organizationMemberDetails.UserID.String())
	if err != nil {
		return err
	}
	displayGetResults(exOrganizationMemberDetails, displayOrganizationMemberDetails)
	return nil
}

func displayOrganizationMemberDetails(member *goclientnew.OrganizationMember) {
	view := tableView()
	view.Append([]string{"User ID", member.UserID.String()})
	view.Append([]string{"External ID", member.ExternalID})
	view.Append([]string{"Display Name", member.DisplayName})
	view.Append([]string{"Username", member.Username})
	view.Append([]string{"Organization ID", member.OrganizationID.String()})
	view.Append([]string{"External Organization ID", member.ExternalOrganizationID})
	view.Render()
}

// TODO: Org Member Serialization is wrong
func apiGetOrganizationMember(userID string) (*goclientnew.OrganizationMember, error) {
	// No params currently
	// newParams := &goclientnew.GetOrganizationMemberParams{}
	orgMemberRes, err := cubClientNew.GetOrganizationMemberWithResponse(ctx, uuid.MustParse(selectedOrganizationID), uuid.MustParse(userID) /*, newParams*/)
	if IsAPIError(err, orgMemberRes) {
		return nil, InterpretErrorGeneric(err, orgMemberRes)
	}
	return orgMemberRes.JSON200, nil
}

func apiGetOrganizationMemberFromUsername(username string) (*goclientnew.OrganizationMember, error) {
	id, err := uuid.Parse(username)
	if err == nil {
		return apiGetOrganizationMember(id.String())
	}
	organizationMembers, err := apiListOrganizationMembers("Username='" + username + "'")
	if err != nil {
		return nil, err
	}
	// find member by userID
	for _, member := range organizationMembers {
		if member.Username == username {
			return member, nil
		}
	}
	return nil, fmt.Errorf("organizationMember %s not found in organization %s", username, selectedOrganizationSlug)
}
