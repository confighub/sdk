// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/spf13/cobra"
)

var unitEventCmd = &cobra.Command{
	Use:               "unit-event",
	Short:             "Unit event commands",
	Long:              `The unit-event subcommands are used to manage unit events`,
	PersistentPreRunE: spacePreRunE,
}

func init() {
	addSpaceFlags(unitEventCmd)
	rootCmd.AddCommand(unitEventCmd)
}