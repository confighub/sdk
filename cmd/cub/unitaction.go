// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/spf13/cobra"
)

var unitActionCmd = &cobra.Command{
	Use:               "unit-action",
	Short:             "Unit action commands",
	Long:              `The unit-action subcommands are used to manage unit actions`,
	PersistentPreRunE: spacePreRunE,
}

func init() {
	addSpaceFlags(unitActionCmd)
	rootCmd.AddCommand(unitActionCmd)
}
