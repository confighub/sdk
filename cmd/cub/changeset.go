// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

var changesetCmd = &cobra.Command{
	Use:   "changeset",
	Short: "ChangeSet commands",
	Long: getCommandHelp(`The changeset subcommands are used to manage changesets.

ChangeSets are explained at https://docs.confighub.com/background/entities/changeset/.
A guide for how to use ChangeSets is at https://docs.confighub.com/guide/change-apply/.
`, ""),
	PersistentPreRunE: spacePreRunE,
}

func init() {
	addSpaceFlags(changesetCmd)
	rootCmd.AddCommand(changesetCmd)
	addExplainCmd(changesetCmd, "ChangeSet")
}

// buildWhereClauseFromChangeSets generates a WHERE clause from changeset identifiers
func buildWhereClauseFromChangeSets(changesetIds []string) (string, error) {
	return buildWhereClauseFromIdentifiers(changesetIds, "ChangeSetID", "Slug")
}

// addChangeSetIDToWhereClause adds changeset constraint to where clause, for reuse across commands
func addChangeSetIDToWhereClause(whereClause, changesetID string) string {
	if changesetID == "" {
		return whereClause
	}
	changesetConstraint := fmt.Sprintf("ChangeSetID = '%s'", changesetID)
	if whereClause != "" {
		return fmt.Sprintf("%s AND %s", whereClause, changesetConstraint)
	}
	return changesetConstraint
}
