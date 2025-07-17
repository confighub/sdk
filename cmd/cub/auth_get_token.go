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
	Long:  `Get the current JWT access token from the session. Useful for scripting and integration with other tools like Claude Code MCP.`,
	Args:  cobra.ExactArgs(0),
	RunE:  authGetTokenCmdRun,
}

func init() {
	authCmd.AddCommand(authGetTokenCmd)
}

func authGetTokenCmdRun(cmd *cobra.Command, args []string) error {
	// Check if we have a valid session
	if authSession.AccessToken == "" {
		return fmt.Errorf("not authenticated. Please run 'cub auth login' first")
	}

	// Simply output the token. Do not include a newline so that it can be used in a HTTP header directly.
	fmt.Print(authSession.AccessToken)

	return nil
}
