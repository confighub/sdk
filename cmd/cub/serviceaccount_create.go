// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/spf13/cobra"
)

var serviceAccountCreateArgs struct {
	permissions []string
	orgRole     string
}

var serviceAccountCreateCmd = &cobra.Command{
	Use:   "create <name>",
	Short: "Create a service account",
	Args:  cobra.ExactArgs(1),
	Long: getCommandHelp(`Create a ServiceAccount entity, and the user it acts as.

The caller is given Manage permission on the new service account. Without --org-role, the service
account has no organization role, so it may do only what is granted to its user.

Examples:
`+"```"+`
  # Create a service account
  cub serviceaccount create deploy-bot

  # Create a service account that can view everything in the organization
  cub serviceaccount create auditor --org-role viewer

  # Create a service account from a JSON definition
  cub serviceaccount create deploy-bot --from-stdin < serviceaccount.json
`+"```"+`
`, ""),
	RunE: serviceAccountCreateCmdRun,
}

func init() {
	addStandardCreateFlags(serviceAccountCreateCmd)
	serviceAccountCreateCmd.Flags().StringSliceVar(&serviceAccountCreateArgs.permissions, "permission", []string{}, permissionCreateHelp)
	serviceAccountCreateCmd.Flags().StringVar(&serviceAccountCreateArgs.orgRole, "org-role", "", "organization-level role for the service account (admin, manager, editor, user, viewer, creator, member, or none), no higher than your own; the default is none, which leaves it with only the permissions granted to its user")
	serviceAccountCmd.AddCommand(serviceAccountCreateCmd)
}

func serviceAccountCreateCmdRun(cmd *cobra.Command, args []string) error {
	if err := validateStdinFlags(); err != nil {
		return err
	}
	if err := ValidateLabelRemoval(label, false); err != nil {
		return err
	}
	if err := ValidateDeleteGateRemoval(deleteGate, false); err != nil {
		return err
	}

	newBody := &goclientnew.ServiceAccount{}
	if flagPopulateModelFromStdin || flagFilename != "" {
		if err := populateModelFromFlags(newBody); err != nil {
			return err
		}
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
	if err := applyPermissions(serviceAccountCreateArgs.permissions, &newBody.Permissions); err != nil {
		return err
	}
	if cmd.Flags().Changed("org-role") {
		newBody.OrgRole = serviceAccountCreateArgs.orgRole
	}

	// Even if slug was set in stdin, we override it with the one from args
	if args[0] == "-" {
		newBody.Slug = randomUnusedSlug()
	} else {
		newBody.Slug = makeSlug(args[0])
	}

	params := &goclientnew.CreateServiceAccountParams{}
	if allowExists {
		allowExistsStr := "true"
		params.AllowExists = &allowExistsStr
	}
	params.DryRun = dryRunParam()
	serviceAccountRes, err := cubClientNew.CreateServiceAccountWithResponse(ctx, params, *newBody)
	if cubapi.IsAPIError(err, serviceAccountRes) {
		return cubapi.InterpretErrorGeneric(err, serviceAccountRes)
	}

	serviceAccountDetails := serviceAccountRes.JSON200
	displayCreateResults(serviceAccountDetails, "service account", newBody.Slug, serviceAccountDetails.ServiceAccountID.String(), displayServiceAccountDetails)
	return nil
}
