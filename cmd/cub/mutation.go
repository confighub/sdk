// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/spf13/cobra"
)

var mutationCmd = &cobra.Command{
	Use:   "mutation",
	Short: "Mutation commands",
	Long: getCommandHelp(`The mutation subcommands are used to manage mutations.
	
Mutations are explained at https://docs.confighub.com/background/entities/mutation/.`, ""),
	PersistentPreRunE: spacePreRunE,
}

func init() {
	addSpaceFlags(mutationCmd)
	rootCmd.AddCommand(mutationCmd)
}
