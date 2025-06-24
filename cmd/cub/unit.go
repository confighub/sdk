// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/spf13/cobra"
)

var unitCmd = &cobra.Command{
	Use:               "unit",
	Short:             "Unit commands",
	Long:              `The unit subcommands are used to manage units`,
	PersistentPreRunE: spacePreRunE,
}

func init() {
	addSpaceFlags(unitCmd)
	rootCmd.AddCommand(unitCmd)
}
