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
bounded by a start and an end Tag created with it. Where it is headed is its InScopeSpaceIDs: a
list given with --in-scope-space, or one the server works out from --where-space-field,
--space-filter, or --component. Given none, it is headed wherever its Links reach. Its UpdateType
says how it propagates: UpgradeUnit (the clone lineage, the default) and MergeUnits follow Links
and take the change from Revisions the source Unit already has; Insert, Upsert, and
TransformPaths carry a change made to Units outside its scope into the Spaces in scope, over Links
of that type; and Invoke runs one Invocation in each Space in scope.

Creating one decides what the change is; promoting one takes it into the Spaces it is headed for:

  cub variant promote my-dev --change-order my-base/release-42

The change order supplies the range, so units it does not cover are passed over and a unit that has
merged past where it starts is an error. A unit short of it merges the revisions before the start
first, unmarked, unless --prior-revisions says Skip or Error. To promote it into the units a
selection picks instead of a whole Space, use "cub unit update --upgrade --change-order".

A change order created with --change-workflow is promoted through that workflow's stages, and the
server holds each promotion to the entry gates of the stage it enters. Without a Space,
"cub variant promote" advances the change into the next stage it has not reached, or into the
stage --target-stage names:

  cub variant promote --change-order my-base/release-42 --target-stage staging

Aborting a change order says the change is not coming to the Spaces still waiting for it, and is
what undoing it requires. It marks what it promoted, so one demote undoes it in a Space however
many revisions it made, and records that it did:

  cub changeorder update my-base/release-42 --aborted-reason "superseded by release-43"
  cub variant demote my-dev --change-order my-base/release-42
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
