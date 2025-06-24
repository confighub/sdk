// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/spf13/cobra"
)

var linkCmd = &cobra.Command{
	Use:               "link",
	Short:             "Link commands",
	Long:              `The link subcommands are used to manage links`,
	PersistentPreRunE: spacePreRunE,
}

func init() {
	addSpaceFlags(linkCmd)
	rootCmd.AddCommand(linkCmd)
}
