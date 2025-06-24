// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/spf13/cobra"
)

var setCmd = &cobra.Command{
	Use:               "set",
	Short:             "Set commands",
	Long:              `The set subcommands are used to manage sets`,
	PersistentPreRunE: spacePreRunE,
}

func init() {
	addSpaceFlags(setCmd)
	rootCmd.AddCommand(setCmd)
}
