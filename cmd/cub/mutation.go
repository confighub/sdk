// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/spf13/cobra"
)

var mutationCmd = &cobra.Command{
	Use:               "mutation",
	Short:             "Mutation commands",
	Long:              `The mutation subcommands are used to manage mutations`,
	PersistentPreRunE: spacePreRunE,
}

func init() {
	addSpaceFlags(mutationCmd)
	rootCmd.AddCommand(mutationCmd)
}
