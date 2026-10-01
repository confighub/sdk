// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/spf13/cobra"
)

var organizationMemberGetCmd = &cobra.Command{
	Use:   "get <organization-member>",
	Short: "Get details about a organization-member",
	Args:  cobra.ExactArgs(1),
	Long: getCommandHelp(`Get detailed information about a organization-member in an organization including its ID, User ID, and Organization ID.

Examples:
`+"```"+`
  # Get details about a organization-member
  cub organization-member get --json my-organization-member

  # Get the member's User ID
  cub organization-member get my-organization-member -o jq=.OrganizationMember.UserID
`+"```"+`
`, ""),
	RunE: organizationMemberGetCmdRun,
}

func init() {
	addStandardGetFlags(organizationMemberGetCmd)
	organizationMemberCmd.AddCommand(organizationMemberGetCmd)
}

func organizationMemberGetCmdRun(cmd *cobra.Command, args []string) error {
	extendedOrganizationMember, err := resolveOrganizationMember(args[0])
	if err != nil {
		return err
	}

	displayGetResults(extendedOrganizationMember, displayExtendedOrganizationMemberDetails)
	return nil
}

// displayExtendedOrganizationMemberDetails renders what get returns: the OrganizationMember
// wrapped the way every other get's entity is, so -o json and -o jq read it as
// .OrganizationMember, as list does.
func displayExtendedOrganizationMemberDetails(extendedOrganizationMember *goclientnew.ExtendedOrganizationMember) {
	displayOrganizationMemberDetails(extendedOrganizationMember.OrganizationMember)
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
