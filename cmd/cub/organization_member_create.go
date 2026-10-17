// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

var organizationMemberCreateCmd = &cobra.Command{
	Use:   "create <organization-member>",
	Short: "Add a user to the organization",
	Args:  cobra.ExactArgs(1),
	Long: getCommandHelp(`Add an existing user to the organization. The user is named in JSON by ExternalID, the user's
ID in the identity provider, read with --from-stdin or --filename; the argument names the member
only in the output. Membership is held by the identity provider, and adding a member requires
an organization admin.

Examples:
`+"```"+`
  # Add a user to the organization by their identity provider ID
  echo '{"ExternalID": "8a1c3d0e-5b7f-4e2a-9c61-0f4b2d7e9a13"}' | cub organization-member create --from-stdin new-member
`+"```"+`
`, ""),
	RunE: organizationMemberCreateCmdRun,
}

func init() {
	addCreateFlagsWithoutDryRun(organizationMemberCreateCmd)
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
	if cubapi.IsAPIError(err, orgMemberRes) {
		return cubapi.InterpretErrorGeneric(err, orgMemberRes)
	}
	organizationMemberDetails := orgMemberRes.JSON200
	displayCreateResults(organizationMemberDetails, "organization-member", args[0], organizationMemberDetails.UserID.String(), displayOrganizationMemberDetails)
	return nil
}
