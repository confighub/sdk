// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/spf13/cobra"
)

var groupCmd = &cobra.Command{
	Use:   "group",
	Short: "Group commands",
	Long:  getCommandHelp(`The group subcommands are used to view the groups you belong to, and to add service accounts to groups and remove them (add, remove). add-worker and remove-worker do the same for workers, which are deprecated. Groups, and the membership of people, are managed in the identity provider.`, ""),
	// just globalPreRun,
}

func init() {
	rootCmd.AddCommand(groupCmd)
	addExplainCmd(groupCmd, "Group")
}
