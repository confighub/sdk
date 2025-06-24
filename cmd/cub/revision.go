// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/spf13/cobra"
)

var revisionCmd = &cobra.Command{
	Use:               "revision",
	Short:             "Revision commands",
	Long:              `The revision subcommands are used to manage revisions`,
	PersistentPreRunE: spacePreRunE,
}

func init() {
	addSpaceFlags(revisionCmd)
	rootCmd.AddCommand(revisionCmd)
}
