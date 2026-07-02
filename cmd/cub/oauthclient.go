// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/spf13/cobra"
)

var oauthClientCmd = &cobra.Command{
	Use:               "oauthclient",
	Short:             "OAuth client commands",
	Long:              getCommandHelp(`The oauthclient subcommands register and manage per-app OAuth clients (RFC 7591) for browser apps that authenticate against the ConfigHub API. Available only when the ConfigHub instance runs its own authorization server.`, ""),
	PersistentPreRunE: organizationPreRunE,
}

func init() {
	rootCmd.AddCommand(oauthClientCmd)
	addExplainCmd(oauthClientCmd, "OAuthClient")
}
