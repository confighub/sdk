// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

var contextListCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"get-contexts"},
	Short:   "List all contexts",
	Long: getCommandHelp(`List all available contexts showing current context, server, organization, user, and default space.

Examples:
`+"```"+`
  # List all contexts
  cub context list

  # List contexts (using alias)
  cub context get-contexts
`+"```"+`
`, ""),
	Args: cobra.NoArgs,
	RunE: contextListCmdRun,
}

func init() {
	addStandardListDisplayFlags(contextListCmd)
	contextCmd.AddCommand(contextListCmd)
}

func contextListCmdRun(_ *cobra.Command, _ []string) error {
	if contextManager == nil {
		return fmt.Errorf("no contexts configured. Use 'cub context create' to create your first context")
	}

	contexts := contextManager.ListContexts()
	if len(contexts) == 0 {
		tprint("No contexts configured")
		tprint("Use 'cub context create' to create your first context")
		return nil
	}

	displayListResults(contexts, func(ctx *Context) string { return ctx.Name }, displayContextList)

	// The CURRENT column marks the persisted current context. When an override
	// (--context flag or $CUB_CONTEXT) is in effect, that marker is not the
	// context commands actually run against, so spell out the active one.
	if activeContextOverrideSource != "" {
		active := contextManager.ActiveContext().Name
		if active != contextManager.CurrentContextName() {
			tprint(fmt.Sprintf("\nActive context is %q, selected by %s (overriding the current context above).",
				active, activeContextOverrideSource))
		}
	}
	return nil
}

func displayContextList(ctxs []*Context) {
	table := tableView()
	table.SetHeader([]string{"CURRENT", "NAME", "SERVER", "ORGANIZATION", "USER", "SPACE"})

	for _, ctx := range ctxs {
		current := ""
		if ctx.Name == contextManager.CurrentContextName() {
			current = "*"
		}

		user := ctx.Coordinate.User
		if user == "" {
			// Try to load token to get user
			if token, err := contextManager.LoadTokenData(ctx); err == nil && token.AccessToken != "" {
				user = "(authenticated)"
			} else {
				user = "(not authenticated)"
			}
		}

		space := ctx.Settings.DefaultSpace
		if space == "" {
			space = "(none)"
		}

		// Show organization name if available, otherwise show ID
		org := ctx.Metadata.OrganizationName
		if org == "" {
			org = ctx.Coordinate.OrganizationID
		}

		table.Append([]string{
			current,
			ctx.Name,
			ctx.Coordinate.ServerURL,
			org,
			user,
			space,
		})
	}

	table.Render()
}
