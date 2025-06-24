// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/spf13/cobra"
)

var functionCmd = &cobra.Command{
	Use:               "function",
	Short:             "Function commands",
	Long:              `The function subcommands are used to discover and execute functions`,
	PersistentPreRunE: spacePreRunE,
}

func init() {
	addSpaceFlags(functionCmd)
	rootCmd.AddCommand(functionCmd)
}
