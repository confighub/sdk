// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

var authGetTokenCmd = &cobra.Command{
	Use:   "get-token",
	Short: "Get the current JWT access token",
	Long:  getCommandHelp(`Get the current JWT access token from the session. Useful for scripting and integration with other tools like Claude Code MCP.`, ""),
	Args:  cobra.ExactArgs(0),
	RunE:  authGetTokenCmdRun,
}

func init() {
	authCmd.AddCommand(authGetTokenCmd)
}

func authGetTokenCmdRun(cmd *cobra.Command, args []string) error {
	ctx := contextManager.ActiveContext()
	tokenData, err := contextManager.LoadTokenData(ctx)
	if err != nil {
		return err
	}
	if tokenData.AccessToken == "" {
		return fmt.Errorf("not authenticated. Please run 'cub auth login' first")
	}

	// Simply output the token. Do not include a newline so that it can be used in a HTTP header directly.
	fmt.Print(tokenData.AccessToken)

	return nil
}
