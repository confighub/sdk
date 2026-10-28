// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/spf13/cobra"
)

var organizationUpdateCmd = &cobra.Command{
	Use:   "update <slug or id>",
	Short: "Update an organization",
	Long: getCommandHelp(`Update an organization.

An organization's permissions grant access to it, and its Children permissions, such as
CreateChildren and ViewChildren, to the spaces and the other entities that belong to the
organization rather than to a space. They do not reach what is in a space: that is the space's
own permissions to grant. Changing them requires Manage permission on the organization.

With --patch, only the fields named are changed, as is usual for labels, annotations and
permissions: a label or annotation given as key=- is removed.

Examples:
`+"```"+`
  # Let a group create spaces
  cub organization update --patch my-org --permission CreateChildren:group:platform

  # Let a user view every space, and no longer manage the organization
  cub organization update --patch my-org --permission ViewChildren:user@example.com --permission -Manage:user@example.com

  # Add a label and remove another
  cub organization update --patch my-org --label tier=enterprise --label trial=-
`+"```"+`
`, ""),
	Args: cobra.ExactArgs(1),
	RunE: organizationUpdateCmdRun,
}

var organizationUpdatePermissions []string

func init() {
	addStandardUpdateFlags(organizationUpdateCmd)
	organizationUpdateCmd.Flags().StringSliceVar(&organizationUpdatePermissions, "permission", []string{}, permissionUpdateHelp)
	organizationUpdateCmd.Flags().BoolVar(&isPatch, "patch", false, "use patch API")
	organizationCmd.AddCommand(organizationUpdateCmd)
}

func organizationUpdateCmdRun(cmd *cobra.Command, args []string) error {
	if err := validateStdinFlags(); err != nil {
		return err
	}

	if isPatch && flagReplace {
		return errors.New("only one of --patch and --replace should be specified")
	}
	if err := ValidateLabelRemoval(label, isPatch); err != nil {
		return err
	}
	if err := ValidateDeleteGateRemoval(deleteGate, isPatch); err != nil {
		return err
	}

	extendedOrganization, err := resolveOrganization(args[0], "*") // get all fields for RMW
	if err != nil {
		return err
	}
	currentOrganization := extendedOrganization.Organization

	if isPatch {
		patchData, err := BuildPatchDataWithPermissions(nil, organizationUpdatePermissions)
		if err != nil {
			return fmt.Errorf("failed to build patch data: %w", err)
		}
		orgRes, err := cubClientNew.PatchOrganizationWithBodyWithResponse(ctx, currentOrganization.OrganizationID,
			&goclientnew.PatchOrganizationParams{DryRun: dryRunParam()}, "application/merge-patch+json", bytes.NewReader(patchData))
		if cubapi.IsAPIError(err, orgRes) {
			return cubapi.InterpretErrorGeneric(err, orgRes)
		}
		organizationDetails := orgRes.JSON200
		displayUpdateResults(organizationDetails, "organization", args[0], organizationDetails.OrganizationID.String(), displayOrganizationDetails)
		return nil
	}
	// Handle --from-stdin or --filename with optional --replace
	if flagPopulateModelFromStdin || flagFilename != "" {
		existingOrganization := currentOrganization
		if flagReplace {
			// Replace mode - create new entity, allow Version to be overwritten
			currentOrganization = new(goclientnew.Organization)
			currentOrganization.Version = existingOrganization.Version
		}

		if err := populateModelFromFlags(currentOrganization); err != nil {
			return err
		}

		// Ensure essential fields can't be clobbered
		currentOrganization.OrganizationID = existingOrganization.OrganizationID
	}
	setDisplayNameAndHiddenReason(&currentOrganization.DisplayName, &currentOrganization.HiddenReason)
	err = setAnnotations(&currentOrganization.Annotations)
	if err != nil {
		return err
	}
	err = setLabels(&currentOrganization.Labels)
	if err != nil {
		return err
	}
	err = setDeleteGates(&currentOrganization.DeleteGates)
	if err != nil {
		return err
	}
	err = applyPermissions(organizationUpdatePermissions, &currentOrganization.Permissions)
	if err != nil {
		return err
	}

	orgRes, err := cubClientNew.UpdateOrganizationWithResponse(ctx, currentOrganization.OrganizationID, &goclientnew.UpdateOrganizationParams{DryRun: dryRunParam()}, *currentOrganization)
	if cubapi.IsAPIError(err, orgRes) {
		return cubapi.InterpretErrorGeneric(err, orgRes)
	}
	organizationDetails := orgRes.JSON200
	displayUpdateResults(organizationDetails, "organization", args[0], organizationDetails.OrganizationID.String(), displayOrganizationDetails)
	return nil
}
