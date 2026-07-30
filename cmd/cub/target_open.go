// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/confighub/sdk/core/cubapi"
	"github.com/spf13/cobra"
)

var targetOpenCmd = &cobra.Command{
	Use:         "open",
	Short:       "Open the target list in the web UI",
	Args:        cobra.NoArgs,
	Annotations: map[string]string{"OrgLevel": ""},
	Long: getCommandHelp(`Open the web UI on the target list.

Examples:
`+"```"+`
  # Open the target list
  cub target open

  # Print the URL instead of opening a browser
  cub target open --print-url
`+"```"+`
`, ""),
	RunE: targetOpenCmdRun,
}

func init() {
	enableOpenFlags(targetOpenCmd)
	targetCmd.AddCommand(targetOpenCmd)
}

func targetOpenCmdRun(_ *cobra.Command, _ []string) error {
	return openWebUI(cubapi.GetTargetListURL(webUIServerURL()))
}
