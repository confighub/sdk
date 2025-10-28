// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/spf13/cobra"
)

var contextGetCmd = &cobra.Command{
	Use:   "get [context-name]",
	Short: "Get context information",
	Long: getCommandHelp(`Get information about a specific context or the current context.

Examples:
`+"```"+`
  # Get current context information
  cub context get

  # Get specific context information
  cub context get prod-acme
`+"```"+`
`, ""),
	Args: cobra.RangeArgs(0, 1),
	RunE: contextGetCmdRun,
}

func init() {
	enableJsonFlag(contextGetCmd)
	enableJqFlag(contextGetCmd)
	contextCmd.AddCommand(contextGetCmd)
}

func contextGetCmdRun(_ *cobra.Command, args []string) error {
	var ctx *Context
	var err error
	if len(args) > 0 {
		ctx, err = contextManager.GetContext(args[0])
		if err != nil {
			return err
		}
	} else {
		ctx = contextManager.ActiveContext()
	}
	displayGetResults(ctx, displayContextDetails)
	return nil
}

func displayContextDetails(ctx *Context) {
	view := detailView()
	view.Append([]string{"Context Name", ctx.Name})
	view.Append([]string{"User", ctx.Coordinate.User})
	view.Append([]string{"Organization ID", ctx.Coordinate.OrganizationID})
	view.Append([]string{"Organization Name", ctx.Metadata.OrganizationName})
	view.Append([]string{"Server URL", ctx.Coordinate.ServerURL})
	view.Append([]string{"Default Space", ctx.Settings.DefaultSpace})
	view.Render()
}
