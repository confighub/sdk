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

var serviceAccountUpdateArgs struct {
	permissions []string
	orgRole     string
}

var serviceAccountUpdateCmd = &cobra.Command{
	Use:   "update [<name or id>]",
	Short: "Update a service account or multiple service accounts",
	Args:  cobra.MaximumNArgs(1),
	Long: getCommandHelp(`Update a ServiceAccount entity.

Changing a service account's organization role or its permissions requires Manage permission on the
service account, and the role cannot be set above your own.

Examples:
`+"```"+`
  # Let another user manage a service account
  cub serviceaccount update --patch deploy-bot --permission Manage:user@example.com

  # Give a service account an organization role
  cub serviceaccount update --patch deploy-bot --org-role viewer

  # Label every service account of a team
  cub serviceaccount update --patch --where "Slug LIKE 'payments-%'" --label team=payments
`+"```"+`

With no name, --patch updates every service account --where selects.
`, ""),
	RunE: serviceAccountUpdateCmdRun,
}

func init() {
	addStandardUpdateFlags(serviceAccountUpdateCmd)
	serviceAccountUpdateCmd.Flags().BoolVar(&isPatch, "patch", false, "use patch API")
	serviceAccountUpdateCmd.Flags().StringSliceVar(&serviceAccountUpdateArgs.permissions, "permission", []string{}, permissionUpdateHelp)
	serviceAccountUpdateCmd.Flags().StringVar(&serviceAccountUpdateArgs.orgRole, "org-role", "", "organization-level role for the service account (admin, manager, editor, user, viewer, creator, member, or none), no higher than your own")
	enableWhereFlag(serviceAccountUpdateCmd)
	serviceAccountCmd.AddCommand(serviceAccountUpdateCmd)
}

func serviceAccountUpdateCmdRun(cmd *cobra.Command, args []string) error {
	if isPatch && flagReplace {
		return errors.New("only one of --patch and --replace should be specified")
	}
	if err := ValidateLabelRemoval(label, isPatch); err != nil {
		return err
	}
	if err := ValidateDeleteGateRemoval(deleteGate, isPatch); err != nil {
		return err
	}
	if err := validateStdinFlags(); err != nil {
		return err
	}
	orgRoleChanged := cmd.Flags().Changed("org-role")
	enhancer := func(patchMap map[string]interface{}) {
		if orgRoleChanged {
			patchMap["OrgRole"] = serviceAccountUpdateArgs.orgRole
		}
	}
	if len(args) == 0 {
		return runBulkServiceAccountUpdate(enhancer)
	}
	if where != "" {
		return errors.New("--where selects the service accounts to update, so it takes no service account name")
	}

	currentEnvelope, err := resolveServiceAccount(args[0], "*") // get all fields for RMW
	if err != nil {
		return err
	}
	currentServiceAccount := currentEnvelope.ServiceAccount
	serviceAccountID := currentServiceAccount.ServiceAccountID

	if isPatch {
		patchData, err := BuildPatchDataWithPermissions(enhancer, serviceAccountUpdateArgs.permissions)
		if err != nil {
			return fmt.Errorf("failed to build patch data: %w", err)
		}
		serviceAccountRes, err := cubClientNew.PatchServiceAccountWithBodyWithResponse(
			ctx,
			serviceAccountID,
			&goclientnew.PatchServiceAccountParams{DryRun: dryRunParam()},
			"application/merge-patch+json",
			bytes.NewReader(patchData),
		)
		if cubapi.IsAPIError(err, serviceAccountRes) {
			return cubapi.InterpretErrorGeneric(err, serviceAccountRes)
		}
		serviceAccountDetails := serviceAccountRes.JSON200
		displayUpdateResults(serviceAccountDetails, "service account", args[0], serviceAccountDetails.ServiceAccountID.String(), displayServiceAccountDetails)
		return nil
	}

	newBody := currentServiceAccount
	if flagPopulateModelFromStdin || flagFilename != "" {
		if flagReplace {
			// Replace mode - create new entity, allow Version to be overwritten
			newBody = new(goclientnew.ServiceAccount)
			newBody.Version = currentServiceAccount.Version
		}
		if err := populateModelFromFlags(newBody); err != nil {
			return err
		}
		// Ensure essential fields can't be clobbered
		newBody.OrganizationID = currentServiceAccount.OrganizationID
		newBody.ServiceAccountID = currentServiceAccount.ServiceAccountID
	}
	setDisplayNameAndHiddenReason(&newBody.DisplayName, &newBody.HiddenReason)
	if err := setAnnotations(&newBody.Annotations); err != nil {
		return err
	}
	if err := setLabels(&newBody.Labels); err != nil {
		return err
	}
	if err := setDeleteGates(&newBody.DeleteGates); err != nil {
		return err
	}
	if err := applyPermissions(serviceAccountUpdateArgs.permissions, &newBody.Permissions); err != nil {
		return err
	}
	if orgRoleChanged {
		newBody.OrgRole = serviceAccountUpdateArgs.orgRole
	}

	serviceAccountRes, err := cubClientNew.UpdateServiceAccountWithResponse(ctx, serviceAccountID, &goclientnew.UpdateServiceAccountParams{DryRun: dryRunParam()}, *newBody)
	if cubapi.IsAPIError(err, serviceAccountRes) {
		return cubapi.InterpretErrorGeneric(err, serviceAccountRes)
	}
	serviceAccountDetails := serviceAccountRes.JSON200
	displayUpdateResults(serviceAccountDetails, "service account", args[0], serviceAccountDetails.ServiceAccountID.String(), displayServiceAccountDetails)
	return nil
}

// runBulkServiceAccountUpdate patches every service account --where selects.
func runBulkServiceAccountUpdate(enhancer func(map[string]interface{})) error {
	if !isPatch {
		return errors.New("--patch is required to update multiple service accounts")
	}
	if where == "" {
		return errors.New("--where is required to update multiple service accounts")
	}
	patchData, err := BuildPatchDataWithPermissions(enhancer, serviceAccountUpdateArgs.permissions)
	if err != nil {
		return fmt.Errorf("failed to build patch data: %w", err)
	}
	params := &goclientnew.BulkPatchServiceAccountsParams{Where: &where}
	params.IncludeHidden = includeHiddenParam()
	params.DryRun = dryRunParam()
	bulkRes, err := cubClientNew.BulkPatchServiceAccountsWithBodyWithResponse(ctx, params,
		"application/merge-patch+json", bytes.NewReader(patchData))
	if cubapi.IsAPIError(err, bulkRes) {
		return cubapi.InterpretErrorGeneric(err, bulkRes)
	}
	return displayBulkGenericCreateOrUpdateResults(
		bulkRes.JSON200, bulkRes.JSON207, bulkRes.StatusCode(), "service account", "update", where,
		func(r *goclientnew.ServiceAccountCreateOrUpdateResponse) *goclientnew.ResponseError { return r.Error },
		func(r *goclientnew.ServiceAccountCreateOrUpdateResponse) string {
			if r.ServiceAccount != nil {
				return fmt.Sprintf("%s (ID: %s)", r.ServiceAccount.Slug, r.ServiceAccount.ServiceAccountID)
			}
			return ""
		},
	)
}
