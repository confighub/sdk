// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

var contextSetCmd = &cobra.Command{
	Use:   "set [context-name] [options]",
	Short: "Change a context's settings",
	Long: getCommandHelp(`Change a setting of the current context or of a named one.

--ui-url is where the context's web UI is served, when that is not the server itself.
The browser commands ("cub <noun> open", "cub auth browser-session") build their links
from it. An empty value clears it, and links go to the server URL again, which is right
wherever the UI is embedded in the server.

Examples:
`+"```"+`
  # The current context's UI is served on its own host
  cub context set --ui-url=https://ui-next.testhub.confighub.net

  # Set it for another context
  cub context set hub-next --ui-url=https://ui-next.testhub.confighub.net

  # Go back to the UI the server embeds
  cub context set --ui-url=
`+"```"+`
`, ""),
	Args: cobra.MaximumNArgs(1),
	RunE: contextSetCmdRun,
}

var setUIURL string

func init() {
	contextSetCmd.Flags().StringVar(&setUIURL, "ui-url", "", "Where this context's web UI is served; empty for the server itself")
	_ = contextSetCmd.MarkFlagRequired("ui-url")

	contextCmd.AddCommand(contextSetCmd)
}

func contextSetCmdRun(_ *cobra.Command, args []string) error {
	ctx := contextManager.ActiveContext()
	if len(args) > 0 {
		named, err := contextManager.GetContext(args[0])
		if err != nil {
			return err
		}
		ctx = named
	}

	uiURL, err := normalizeUIURL(setUIURL)
	if err != nil {
		return err
	}
	ctx.Settings.UIURL = uiURL

	if err := contextManager.SaveConfig(); err != nil {
		return fmt.Errorf("failed to save context: %w", err)
	}

	if uiURL == "" {
		tprint("Context %q opens the web UI at its server, %s", ctx.Name, ctx.Coordinate.ServerURL)
		return nil
	}
	tprint("Context %q opens the web UI at %s", ctx.Name, uiURL)
	return nil
}
