// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/spf13/cobra"
)

var targetCmd = &cobra.Command{
	Use:   "target",
	Short: "Target commands",
	Long: getCommandHelp(`The target subcommands are used to manage targets.

Targets are explained at https://docs.confighub.com/background/entities/target/.`, ""),
	PersistentPreRunE: spacePreRunE,
}

func init() {
	addSpaceFlags(targetCmd)
	rootCmd.AddCommand(targetCmd)
}
