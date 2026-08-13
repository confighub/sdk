// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

var contextUseCmd = &cobra.Command{
	Use:     "use [name]",
	Aliases: []string{"set-current"},
	Short:   "Switch to a different context",
	Long: getCommandHelp(`Switch to a different context making it the current active context.

Examples:
`+"```"+`
  # Switch to a context
  cub context use prod-acme

  # Switch using alias
  cub context set-current staging
`+"```"+`
`, ""),
	Args: cobra.ExactArgs(1),
	RunE: contextUseCmdRun,
}

func init() {
	contextCmd.AddCommand(contextUseCmd)
}

func contextUseCmdRun(_ *cobra.Command, args []string) error {
	if contextManager == nil {
		return fmt.Errorf("context manager not initialized")
	}

	name := args[0]

	// Set the current context
	if err := contextManager.SetCurrentContext(name); err != nil {
		return err
	}

	// Save the configuration
	if err := contextManager.SaveConfig(); err != nil {
		return fmt.Errorf("failed to save context: %w", err)
	}

	// Get the context to show details
	ctx, err := contextManager.GetContext(name)
	if err != nil {
		return err
	}

	tprint("Switched to context %q", name)

	// Show context details
	fmt.Printf("Server: %s\n", ctx.Coordinate.ServerURL)
	fmt.Printf("Organization: %s\n", ctx.Coordinate.OrganizationID)
	fmt.Printf("Default Space: %s\n", ctx.Settings.DefaultSpace)
	if ctx.Coordinate.User != "" {
		fmt.Printf("User: %s\n", ctx.Coordinate.User)
	}

	// Check if authenticated
	if _, err := contextManager.LoadTokenData(ctx); err != nil {
		fmt.Println("\nNote: This context is not authenticated.")
		fmt.Printf("Run 'cub auth login' to authenticate.\n")
	}

	return nil
}
