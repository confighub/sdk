// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/spf13/cobra"
)

var userCmd = &cobra.Command{
	Use:   "user",
	Short: "User commands",
	Long:  `The user subcommands are used to view users`,
	// just globalPreRun,
}

func init() {
	rootCmd.AddCommand(userCmd)
}
