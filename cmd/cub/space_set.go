// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/spf13/cobra"
)

var spaceSetCmd = &cobra.Command{
	Use:   "set <space>",
	Short: "Sets a space as the current space",
	Args:  cobra.ExactArgs(1),
	Long:  `Sets a space as the current space`,
	RunE:  spaceSetCmdRun,
}

func init() {
	spaceCmd.AddCommand(spaceSetCmd)
}

func spaceSetCmdRun(cmd *cobra.Command, args []string) error {
	return nil
}
