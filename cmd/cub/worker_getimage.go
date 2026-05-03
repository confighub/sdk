// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

var workerGetImageCmd = &cobra.Command{
	Use:   "get-image",
	Args:  cobra.NoArgs,
	Short: "Print the default Bridge Worker container image",
	Long: getCommandHelp(`Print the default Bridge Worker container image used by `+"`cub worker install`"+` when --image is not specified.

The image tag is derived from the connected server's version (falling back to :latest).
`, ""),
	RunE: workerGetImageCmdRun,
}

func init() {
	workerCmd.AddCommand(workerGetImageCmd)
}

func workerGetImageCmdRun(_ *cobra.Command, _ []string) error {
	fmt.Println(getWorkerImage(""))
	return nil
}
