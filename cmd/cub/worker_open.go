// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/confighub/sdk/core/cubapi"
	"github.com/spf13/cobra"
)

var workerOpenCmd = &cobra.Command{
	Use:         "open",
	Short:       "Open the bridge worker list in the web UI",
	Args:        cobra.NoArgs,
	Annotations: map[string]string{"OrgLevel": ""},
	Long: getCommandHelp(`Open the web UI on the bridge worker list.

Examples:
`+"```"+`
  # Open the bridge worker list
  cub worker open

  # Print the URL instead of opening a browser
  cub worker open --print-url
`+"```"+`
`, ""),
	RunE: workerOpenCmdRun,
}

func init() {
	enableOpenFlags(workerOpenCmd)
	workerCmd.AddCommand(workerOpenCmd)
}

func workerOpenCmdRun(_ *cobra.Command, _ []string) error {
	return openWebUI(cubapi.GetWorkerListURL(webUIServerURL()))
}
