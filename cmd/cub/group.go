// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/spf13/cobra"
)

var groupCmd = &cobra.Command{
	Use:   "group",
	Short: "Group commands",
	Long:  getCommandHelp(`The group subcommands are used to view the groups you belong to. Groups are managed in the identity provider.`, ""),
	// just globalPreRun,
}

func init() {
	rootCmd.AddCommand(groupCmd)
	addExplainCmd(groupCmd, "Group")
}
