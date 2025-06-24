// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/spf13/cobra"
)

var targetCmd = &cobra.Command{
	Use:               "target",
	Short:             "Target commands",
	Long:              `The target subcommands are used to manage targets`,
	PersistentPreRunE: spacePreRunE,
}

func init() {
	addSpaceFlags(targetCmd)
	rootCmd.AddCommand(targetCmd)
}
