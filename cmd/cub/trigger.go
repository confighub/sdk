// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/spf13/cobra"
)

var triggerCmd = &cobra.Command{
	Use:               "trigger",
	Short:             "Trigger commands",
	Long:              `The trigger subcommands are used to manage triggers`,
	PersistentPreRunE: spacePreRunE,
}

func init() {
	addSpaceFlags(triggerCmd)
	rootCmd.AddCommand(triggerCmd)
}
