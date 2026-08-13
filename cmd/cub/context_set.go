// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

var contextSetCmd = &cobra.Command{
	Use:   "set [context-name] [options]",
	Short: "Set the default space for a context",
	Long: getCommandHelp(`Set the default space for the current context or a specific context.

This command updates the default space that will be used for commands
when no --space flag is provided.

Examples:
`+"```"+`
  # Set default space for current context
  cub context set --space=production

  # Set default space for a specific context
  cub context set prod-context --space=production

  # Set default space to development
  cub context set --space=dev
`+"```"+`
`, ""),
	Args: cobra.MaximumNArgs(1),
	RunE: contextSetCmdRun,
}

var (
	setSpace string
)

func init() {
	// Only support --space flag
	contextSetCmd.Flags().StringVar(&setSpace, "space", "", "Default space slug")
	contextSetCmd.MarkFlagRequired("space")

	contextCmd.AddCommand(contextSetCmd)
}

func contextSetCmdRun(_ *cobra.Command, args []string) error {
	var ctx *Context
	var err error

	// If a context name was provided, use that context
	if len(args) > 0 {
		ctx, err = contextManager.GetContext(args[0])
		if err != nil {
			return err
		}
	} else {
		ctx = contextManager.ActiveContext()
	}

	// Check if we're authenticated by trying to load the token
	isAuthenticated := false
	if token, err := contextManager.LoadTokenData(ctx); err == nil && token.AccessToken != "" {
		isAuthenticated = true
	}

	// A wildcard ("*") or empty default space selects cross-space (org-level)
	// operations by default; there is no space to look up, so set it directly.
	if setSpace == "*" || setSpace == "" {
		ctx.Settings.DefaultSpace = "*"
		if ctx.Name == contextManager.ActiveContext().Name {
			selectedSpaceID = setSpace
			selectedSpaceSlug = setSpace
		}
	} else if isAuthenticated {
		// Only try to verify the space exists if we're authenticated
		space, err := apiGetSpaceFromSlug(setSpace, "")
		if err != nil {
			// Even though we're authenticated, the space might not exist
			// or there could be a network issue
			tprint("Warning: Could not verify space %q exists: %v", setSpace, err)
			tprint("Setting space to %q anyway", setSpace)
			ctx.Settings.DefaultSpace = setSpace
		} else {
			// Space was found, use the canonical slug from the API
			ctx.Settings.DefaultSpace = space.Slug

			// Update global variables for immediate effect only if we modified the current context
			if ctx.Name == contextManager.ActiveContext().Name {
				selectedSpaceID = space.SpaceID.String()
				selectedSpaceSlug = space.Slug
			}
		}
	} else {
		// Not authenticated - just use the provided space name
		tprint("Note: Cannot verify space exists (not authenticated). Setting space to %q", setSpace)
		ctx.Settings.DefaultSpace = setSpace
	}

	// Save the configuration
	if err := contextManager.SaveConfig(); err != nil {
		return fmt.Errorf("failed to save context: %w", err)
	}

	tprint("Default space for context %q set to %q", ctx.Name, ctx.Settings.DefaultSpace)
	return nil
}
