// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

var contextListCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"get-contexts"},
	Short:   "List all contexts",
	Long: getCommandHelp(`List all available contexts showing current context, server, organization, user, and default space.

SELECTED names everything that asked for a context — the --context flag, the
CUB_CONTEXT environment variable, config.yaml — and stars the one in effect, which
is the highest-precedence of them in that order.

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
	return nil
}

func displayContextList(ctxs []*Context) {
	header, rows := contextListTable(ctxs)
	table := tableView()
	table.SetHeader(header)
	for _, row := range rows {
		table.Append(row)
	}
	table.Render()
}

// contextListTable builds the header and rows of the context listing. SELECTED
// names every source that asked for a context — config.yaml, $CUB_CONTEXT, the
// --context flag — and stars the one in effect, which is the highest-precedence
// of them. Showing only the winner would leave a reader unable to tell a
// $CUB_CONTEXT that was obeyed from one that was outranked; showing them without
// the star would leave them to apply the precedence rules by hand, which is the
// ambiguity this listing exists to settle.
func contextListTable(ctxs []*Context) (header []string, rows [][]string) {
	active := contextManager.ActiveContextName()

	header = []string{"SELECTED", "NAME", "SERVER", "ORGANIZATION", "USER", "SPACE"}

	for _, ctx := range ctxs {
		selected := contextSelectedBy(ctx.Name, ctx.Name == active)

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

		rows = append(rows, []string{
			selected,
			ctx.Name,
			ctx.Coordinate.ServerURL,
			org,
			user,
			space,
		})
	}

	return header, rows
}

// contextSelectedBy renders the SELECTED cell for a context: the sources that
// named it, highest precedence first, starred when it is the one in effect. A
// context nothing named gets an empty cell.
func contextSelectedBy(name string, inEffect bool) string {
	var sources []string
	for _, sel := range activeContextSelectors {
		if sel.Context == name {
			sources = append(sources, sel.Source)
		}
	}
	if len(sources) == 0 {
		return ""
	}
	// The two spaces align the unstarred rows under the starred one.
	mark := "  "
	if inEffect {
		mark = "* "
	}
	return mark + strings.Join(sources, ", ")
}
