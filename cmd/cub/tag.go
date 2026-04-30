// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/spf13/cobra"
)

var tagCmd = &cobra.Command{
	Use:   "tag",
	Short: "Tag commands",
	Long: getCommandHelp(`The tag subcommands are used to manage tags.

Tags are explained at https://docs.confighub.com/background/entities/tag/.`, ""),
	PersistentPreRunE: spacePreRunE,
}

func init() {
	addSpaceFlags(tagCmd)
	rootCmd.AddCommand(tagCmd)
	addExplainCmd(tagCmd, "Tag")
}

// buildWhereClauseFromTags generates a WHERE clause from tag identifiers
func buildWhereClauseFromTags(tagIds []string) (string, error) {
	return buildWhereClauseFromIdentifiers(tagIds, "TagID", "Slug")
}
