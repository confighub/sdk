// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/spf13/cobra"
)

var serviceAccountGetCmd = &cobra.Command{
	Use:   "get <name or id>",
	Short: "Get details about a service account",
	Args:  cobra.ExactArgs(1),
	Long: getCommandHelp(`Get detailed information about a ServiceAccount entity, including the user it acts as.

Examples:
`+"```"+`
  # Get service account details in table format
  cub serviceaccount get deploy-bot

  # Get the ID of the user the service account acts as, to grant it access
  cub serviceaccount get deploy-bot -o jq=.ServiceAccount.UserID
`+"```"+`
`, ""),
	RunE: serviceAccountGetCmdRun,
}

func init() {
	addStandardGetFlags(serviceAccountGetCmd)
	serviceAccountCmd.AddCommand(serviceAccountGetCmd)
}

func serviceAccountGetCmdRun(cmd *cobra.Command, args []string) error {
	extendedServiceAccount, err := resolveServiceAccount(args[0], selectFields)
	if err != nil {
		return err
	}
	displayGetResults(extendedServiceAccount, displayExtendedServiceAccountDetails)
	return nil
}

func displayExtendedServiceAccountDetails(extendedServiceAccount *goclientnew.ExtendedServiceAccount) {
	displayServiceAccountDetails(extendedServiceAccount.ServiceAccount)
}

func displayServiceAccountDetails(serviceAccount *goclientnew.ServiceAccount) {
	view := tableView()
	view.Append([]string{"ID", serviceAccount.ServiceAccountID.String()})
	view.Append([]string{"Name", serviceAccount.Slug})
	view.Append([]string{"Display Name", serviceAccount.DisplayName})
	view.Append([]string{"User ID", serviceAccount.UserID.String()})
	view.Append([]string{"Org Role", serviceAccount.OrgRole})
	view.Append([]string{"Created At", serviceAccount.CreatedAt.String()})
	view.Append([]string{"Updated At", serviceAccount.UpdatedAt.String()})
	view.Append([]string{"Labels", labelsToString(serviceAccount.Labels)})
	view.Append([]string{"Delete Gates", deleteGatesToString(serviceAccount.DeleteGates)})
	view.Append([]string{"Annotations", annotationsToString(serviceAccount.Annotations)})
	view.Append([]string{"Permissions", permissionsToString(serviceAccount.Permissions)})
	view.Append([]string{"Organization ID", serviceAccount.OrganizationID.String()})
	view.Render()
}
