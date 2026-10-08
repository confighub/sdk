// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"errors"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/spf13/cobra"
)

var serviceAccountDeleteCmd = &cobra.Command{
	Use:   "delete [<name or id>]",
	Short: "Delete a service account or multiple service accounts",
	Args:  cobra.MaximumNArgs(1),
	Long: getCommandHelp(`Delete a ServiceAccount entity, or the ServiceAccounts --where selects.

Deleting a service account deletes the keys registered to its user and disables the user. The user
is kept, so that what it did stays on record.

Examples:
`+"```"+`
  cub serviceaccount delete deploy-bot

  # Delete the service accounts of a team
  cub serviceaccount delete --where "Labels.team = 'payments'"
`+"```"+`
`, ""),
	RunE: serviceAccountDeleteCmdRun,
}

func init() {
	addStandardDeleteFlags(serviceAccountDeleteCmd)
	enableWhereFlag(serviceAccountDeleteCmd)
	serviceAccountCmd.AddCommand(serviceAccountDeleteCmd)
}

func runBulkServiceAccountDelete() error {
	params := &goclientnew.BulkDeleteServiceAccountsParams{Where: &where}
	params.IncludeHidden = includeHiddenParam()
	bulkRes, err := cubClientNew.BulkDeleteServiceAccountsWithResponse(ctx, params)
	if cubapi.IsAPIError(err, bulkRes) {
		return cubapi.InterpretErrorGeneric(err, bulkRes)
	}
	return displayBulkDeleteResults(bulkRes.JSON200, bulkRes.JSON207, bulkRes.StatusCode(), "service account", deleteOperationName(), where)
}

func serviceAccountDeleteCmdRun(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		if where == "" {
			return errors.New("name a service account, or select service accounts with --where")
		}
		return runBulkServiceAccountDelete()
	}
	if where != "" {
		return errors.New("--where can only be specified with no positional arguments")
	}
	serviceAccountDetails, err := resolveServiceAccount(args[0], "ServiceAccountID,Slug")
	if err != nil {
		return err
	}
	serviceAccountID := serviceAccountDetails.ServiceAccount.ServiceAccountID
	deleteRes, err := cubClientNew.DeleteServiceAccountWithResponse(ctx, serviceAccountID)
	if cubapi.IsAPIError(err, deleteRes) {
		return cubapi.InterpretErrorGeneric(err, deleteRes)
	}
	displayDeleteResults("service account", args[0], serviceAccountID.String(), deleteRes)
	return nil
}
