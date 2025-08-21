// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/spf13/cobra"
)

var tagCmd = &cobra.Command{
	Use:               "tag",
	Short:             "Tag commands",
	Long:              `The tag subcommands are used to manage tags`,
	PersistentPreRunE: spacePreRunE,
}

func init() {
	addSpaceFlags(tagCmd)
	rootCmd.AddCommand(tagCmd)
}

// buildWhereClauseFromTags generates a WHERE clause from tag identifiers
func buildWhereClauseFromTags(tagIds []string) (string, error) {
	return buildWhereClauseFromIdentifiers(tagIds, "TagID", "Slug")
}