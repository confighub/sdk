// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

var changeorderCmd = &cobra.Command{
	Use:   "changeorder",
	Short: "ChangeOrder commands",
	Long: getCommandHelp(`The changeorder subcommands are used to manage change orders.

A ChangeSet names a change within one Space and locks the Units it is open on. A ChangeOrder names
the change itself: the ChangeSets a change creates as it is promoted from Space to Space all carry
the same ChangeOrderID, which is what lets a fleet be asked where a change has landed.

A ChangeOrder resides in a Space, by convention the one holding the changes to promote, and is
bounded by a start and an end Tag created with it. It selects where it is headed with a Filter over
Spaces, and names the Link UpdateType to follow (UpgradeUnit, the clone lineage).
`, ""),
	PersistentPreRunE: spacePreRunE,
}

func init() {
	addSpaceFlags(changeorderCmd)
	rootCmd.AddCommand(changeorderCmd)
	addExplainCmd(changeorderCmd, "ChangeOrder")
}

// buildWhereClauseFromChangeOrders generates a WHERE clause from changeorder identifiers
func buildWhereClauseFromChangeOrders(changeorderIds []string) (string, error) {
	return buildWhereClauseFromIdentifiers(changeorderIds, "ChangeOrderID", "Slug")
}

// addChangeOrderIDToWhereClause adds changeorder constraint to where clause, for reuse across commands
func addChangeOrderIDToWhereClause(whereClause, changeorderID string) string {
	if changeorderID == "" {
		return whereClause
	}
	changeorderConstraint := fmt.Sprintf("ChangeOrderID = '%s'", changeorderID)
	if whereClause != "" {
		return fmt.Sprintf("%s AND %s", whereClause, changeorderConstraint)
	}
	return changeorderConstraint
}
