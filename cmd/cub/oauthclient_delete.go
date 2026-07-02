// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/confighub/sdk/core/cubapi"
	"github.com/spf13/cobra"
)

var oauthClientDeleteCmd = &cobra.Command{
	Use:   "delete <name or client id>",
	Short: "Delete an OAuth client",
	Long:  getCommandHelp(`Delete an OAuth client registered in your organization. Apps using this client_id will no longer be able to log in.`, ""),
	Args:  cobra.ExactArgs(1),
	RunE:  oauthClientDeleteCmdRun,
}

func init() {
	addStandardDeleteFlags(oauthClientDeleteCmd)
	oauthClientCmd.AddCommand(oauthClientDeleteCmd)
}

func oauthClientDeleteCmdRun(cmd *cobra.Command, args []string) error {
	client, err := apiGetOAuthClient(args[0])
	if err != nil {
		return err
	}
	deleteRes, err := cubClientNew.DeleteOAuthClientWithResponse(ctx, client.ClientID)
	if cubapi.IsAPIError(err, deleteRes) {
		return cubapi.InterpretErrorGeneric(err, deleteRes)
	}
	displayDeleteResults("oauthclient", args[0], client.ClientID, deleteRes)
	return nil
}
