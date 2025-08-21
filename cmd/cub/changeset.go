// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/spf13/cobra"
)

var changesetCmd = &cobra.Command{
	Use:               "changeset",
	Short:             "ChangeSet commands",
	Long:              `The changeset subcommands are used to manage changesets`,
	PersistentPreRunE: spacePreRunE,
}

func init() {
	addSpaceFlags(changesetCmd)
	rootCmd.AddCommand(changesetCmd)
}

// buildWhereClauseFromChangeSets generates a WHERE clause from changeset identifiers
func buildWhereClauseFromChangeSets(changesetIds []string) (string, error) {
	return buildWhereClauseFromIdentifiers(changesetIds, "ChangeSetID", "Slug")
}