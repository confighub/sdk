// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
)

var contextGetCmd = &cobra.Command{
	Use:   "get [context-name]",
	Short: "Get context information",
	Long: getCommandHelp(`Get information about a specific context or the current context.

This command does not contact the server. It reads the locally stored context and the
saved access token, and reports a "Token Status" of valid or expired by inspecting the
token's expiration claim locally. To verify authentication against the server, use
'cub auth status' instead.

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
	addStandardDisplayFlags(contextGetCmd)
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
	view.Append([]string{"Token Status", localTokenStatus(ctx)})
	// Attribute the context this invocation runs against — config.yaml, or the
	// override that outranked it, named along with what it outranked — so neither
	// the active context nor an ignored override is a surprise.
	if summary := contextSelectionSummary(); summary != "" && ctx.Name == contextManager.ActiveContextName() {
		view.Append([]string{"Selected By", summary})
	}
	view.Render()
}

// localTokenStatus reports whether the saved access token for ctx is valid or expired,
// determined locally from the token's expiration claim without contacting the server.
// Use 'cub auth status' to verify the token against the server.
func localTokenStatus(ctx *Context) string {
	tokenData, err := contextManager.LoadTokenData(ctx)
	if err != nil || tokenData.AccessToken == "" {
		return "none (run 'cub auth login')"
	}
	expiry, ok := tokenExpiry(tokenData.AccessToken)
	if !ok {
		return "unknown"
	}
	if time.Now().After(expiry) {
		return fmt.Sprintf("expired at %s (run 'cub auth login')", expiry.Format(time.RFC3339))
	}
	return fmt.Sprintf("valid until %s", expiry.Format(time.RFC3339))
}
