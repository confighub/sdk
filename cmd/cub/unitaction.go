// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/spf13/cobra"
)

var unitActionCmd = &cobra.Command{
	Use:   "unit-action",
	Short: "Unit action commands",
	Long: getCommandHelp(`The unit-action subcommands are used to manage unit actions.

UnitActions are explained at https://docs.confighub.com/background/entities/unit-action/.`, ""),
	PersistentPreRunE: spacePreRunE,
}

func init() {
	addSpaceFlags(unitActionCmd)
	rootCmd.AddCommand(unitActionCmd)
}
