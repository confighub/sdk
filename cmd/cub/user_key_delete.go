// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var userKeyDeleteCmd = &cobra.Command{
	Use:   "delete <kid>",
	Short: "Remove a public key from an identity",
	Long: getCommandHelp(`Remove one registered public key, by its thumbprint.

This is the second half of rotation and the response to a private key you no
longer trust. Whatever holds that private key stops being able to authenticate;
anything else the identity has registered keeps working.

To rotate without an outage, add the new key first, restart whatever uses it,
confirm the new key's Last-Used is moving, and only then delete the old one.

Examples:
`+"```"+`
  # Retire a key by thumbprint
  cub user key delete --worker my-worker NzbLsXh8uDCcd-6MNwXF4W_7noWXFZAfHkxZsRGC9Xs
`+"```"+`
`, ""),
	Args: cobra.ExactArgs(1),
	RunE: userKeyDeleteCmdRun,
}

func init() {
	userKeyCmd.AddCommand(userKeyDeleteCmd)
}

func userKeyDeleteCmdRun(cmd *cobra.Command, args []string) error {
	targetUserID, err := resolveKeyTargetUser()
	if err != nil {
		return err
	}

	kid := args[0]
	response, err := apiDeleteUserKey(targetUserID, kid)
	if err != nil {
		return err
	}
	displayDeleteResults("key", kid, kid, response)
	return nil
}

func apiDeleteUserKey(userID uuid.UUID, kid string) (*goclientnew.DeleteResponse, error) {
	credentialRes, err := cubClientNew.DeleteUserKeyWithResponse(ctx, userID.String(), kid)
	if cubapi.IsAPIError(err, credentialRes) {
		return nil, cubapi.InterpretErrorGeneric(err, credentialRes)
	}
	return credentialRes.JSON200, nil
}
