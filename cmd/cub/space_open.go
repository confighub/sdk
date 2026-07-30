// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/confighub/sdk/core/cubapi"
	"github.com/spf13/cobra"
)

var spaceOpenCmd = &cobra.Command{
	Use:   "open [<name or id>]",
	Short: "Open spaces in the web UI",
	Args:  cobra.MaximumNArgs(1),
	Long: getCommandHelp(`Open the web UI on a space, or on the space list when no space is named.

Examples:
`+"```"+`
  # Open the space list
  cub space open

  # Open a specific space
  cub space open my-space

  # Print the URL instead of opening a browser
  cub space open my-space --print-url
`+"```"+`
`, ""),
	RunE: spaceOpenCmdRun,
}

func init() {
	enableOpenFlags(spaceOpenCmd)
	spaceCmd.AddCommand(spaceOpenCmd)
}

func spaceOpenCmdRun(_ *cobra.Command, args []string) error {
	if len(args) == 0 {
		return openWebUI(cubapi.GetSpaceListURL(webUIServerURL()))
	}
	space, err := apiGetSpaceFromSlug(args[0], "SpaceID")
	if err != nil {
		return err
	}
	return openWebUI(cubapi.GetSpaceDetailURL(webUIServerURL(), space.SpaceID.String()))
}
