// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/spf13/cobra"
)

var groupCmd = &cobra.Command{
	Use:   "group",
	Short: "Group commands",
	Long:  getCommandHelp(`The group subcommands are used to view the groups you belong to, and to add workers to groups. Groups, and the membership of everyone but workers, are managed in the identity provider.`, ""),
	// just globalPreRun,
}

func init() {
	rootCmd.AddCommand(groupCmd)
	addExplainCmd(groupCmd, "Group")
}
