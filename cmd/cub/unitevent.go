// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/spf13/cobra"
)

var unitEventCmd = &cobra.Command{
	Use:   "unit-event",
	Short: "Unit event commands",
	Long: getCommandHelp(`The unit-event subcommands are used to manage unit events.

UnitEvents are explained at https://docs.confighub.com/background/entities/unit-event/.`, ""),
	PersistentPreRunE: spacePreRunE,
}

func init() {
	addSpaceFlags(unitEventCmd)
	rootCmd.AddCommand(unitEventCmd)
}
