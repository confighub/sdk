// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

var contextCreateCmd = &cobra.Command{
	Use:   "create [name]",
	Short: "Create a new context",
	Long: getCommandHelp(`Create a new context. If no name is provided, the context will be given a random name.

Examples:
`+"```"+`
  # Create with parameters
  cub context create my-context \
    --server=https://api.confighub.com \
    --space=default

  # Create from current authentication
  cub context create staging --from-current
`+"```"+`
`, ""),
	Args: cobra.RangeArgs(0, 1),
	RunE: contextCreateCmdRun,
}

var (
	createServer       string
	createOrganization string
	createSpace        string
)

func init() {
	contextCreateCmd.Flags().StringVar(&createServer, "server", "", "API server URL")
	contextCreateCmd.Flags().StringVar(&createOrganization, "organization", "", "WorkOS organization ID (optional)")
	contextCreateCmd.Flags().StringVar(&createSpace, "space", "", "Default space (optional)")

	contextCmd.AddCommand(contextCreateCmd)
}

func contextCreateCmdRun(_ *cobra.Command, args []string) error {
	if contextManager == nil {
		return fmt.Errorf("context manager not initialized")
	}

	name := ""
	if len(args) > 0 {
		name = args[0]
	}

	// Create the context
	ctx, err := contextManager.CreateContext(name, createServer, createOrganization, createSpace)
	if err != nil {
		return err
	}

	// Make it the current context
	contextManager.SetCurrentContext(ctx.Name)

	err = contextManager.SaveConfig()
	if err != nil {
		return fmt.Errorf("failed to save context: %w", err)
	}

	tprint("New context %s is now the current context", ctx.Name)
	tprint("Authenticate with the command: cub auth login")

	return nil
}
