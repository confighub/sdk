// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/google/uuid"
	"github.com/spf13/cobra"

	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
)

var organizationMemberCreateCmd = &cobra.Command{
	Use:   "create <organization-member>",
	Short: "Create a organization-member",
	Args:  cobra.ExactArgs(1),
	Long: `Create a new organization-member as a collaborative context for a project or team.

Examples:
  # Create a new organization-member named "my-organization-member" with verbose output, reading configuration from stdin
  # Verbose output prints the details of the created entity
  cub organization-member create --verbose --json --from-stdin my-organization-member

  # Create a new organization-member with minimal output
  cub organization-member create my-organization-member`,
	RunE: organizationMemberCreateCmdRun,
}

func init() {
	addStandardCreateFlags(organizationMemberCreateCmd)
	organizationMemberCmd.AddCommand(organizationMemberCreateCmd)
}

func organizationMemberCreateCmdRun(cmd *cobra.Command, args []string) error {
	if err := validateStdinFlags(); err != nil {
		return err
	}

	member := &goclientnew.OrganizationMember{}
	if flagPopulateModelFromStdin || flagFilename != "" {
		if err := populateModelFromFlags(member); err != nil {
			return err
		}
	}

	orgMemberRes, err := cubClientNew.CreateOrganizationMemberWithResponse(ctx,
		uuid.MustParse(selectedOrganizationID), *member)
	if IsAPIError(err, orgMemberRes) {
		return InterpretErrorGeneric(err, orgMemberRes)
	}
	organizationMemberDetails := orgMemberRes.JSON200
	displayCreateResults(organizationMemberDetails, "organization-member", args[0], organizationMemberDetails.UserID.String(), displayOrganizationMemberDetails)
	return nil
}
