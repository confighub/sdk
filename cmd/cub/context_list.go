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
	if contextManager == nil || contextManager.config == nil {
		return fmt.Errorf("no contexts configured. Use 'cub context create' to create your first context")
	}

	contexts := contextManager.ListContexts()
	if len(contexts) == 0 {
		tprint("No contexts configured")
		tprint("Use 'cub context create' to create your first context")
		return nil
	}

	displayListResults(contexts, func(ctx *Context) string { return ctx.Name }, displayContextList)
	return nil
}

func displayContextList(ctxs []*Context) {
	table := tableView()
	table.SetHeader([]string{"CURRENT", "NAME", "SERVER", "ORGANIZATION", "USER", "SPACE"})

	for _, ctx := range ctxs {
		current := ""
		if ctx.Name == contextManager.config.CurrentContext {
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
