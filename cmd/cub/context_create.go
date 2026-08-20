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

The current context does not change: the new context is added, and everything else keeps
talking to the server it was already talking to. Authenticate the new one by naming it,
and adopt it as the current context when you want to, or ask for that up front with --use.

Examples:
`+"```"+`
  # Create with parameters
  cub context create my-context \
    --server=https://api.confighub.com \
    --space=default

  # Create one for a second server and authenticate it, still working in the current one
  cub context create local-9091 --server=http://localhost:9091
  cub --context local-9091 auth login

  # Create it and switch to it
  cub context create staging --server=https://api.confighub.com --use
`+"```"+`
`, ""),
	Args: cobra.RangeArgs(0, 1),
	RunE: contextCreateCmdRun,
}

var (
	createServer       string
	createOrganization string
	createSpace        string
	createUse          bool
)

func init() {
	contextCreateCmd.Flags().StringVar(&createServer, "server", "", "API server URL")
	contextCreateCmd.Flags().StringVar(&createOrganization, "organization", "", "WorkOS organization ID (optional)")
	contextCreateCmd.Flags().StringVar(&createSpace, "space", "", "Default space (optional)")
	contextCreateCmd.Flags().BoolVar(&createUse, "use", false, "Also make the new context the current one")

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

	// Adding a context says nothing about what anything should be talking to. The current
	// context is one setting shared by every shell and script on the machine, so switching it
	// here would move work that is under way -- to a server that has not been authenticated
	// to yet, at that. A context for a second server, or for another organization, is created
	// while work continues in the current one, and adopted when someone says so.
	//
	// The very first context is the exception, and cubapi.Store handles it: there has to be a
	// current context, so the first one to exist becomes it.
	if createUse {
		if err := contextManager.SetCurrentContext(ctx.Name); err != nil {
			return err
		}
	}

	err = contextManager.SaveConfig()
	if err != nil {
		return fmt.Errorf("failed to save context: %w", err)
	}

	current := contextManager.CurrentContextName()
	if current == ctx.Name {
		tprint("New context %s is now the current context", ctx.Name)
		tprint("Authenticate with the command: cub auth login")
		return nil
	}
	tprint("Created context %s; %s is still the current context", ctx.Name, current)
	tprint("Authenticate it with the command: cub --context %s auth login", ctx.Name)
	tprint("Make it the current context with: cub context use %s", ctx.Name)

	return nil
}
