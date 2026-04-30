// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/spf13/cobra"
)

var triggerCmd = &cobra.Command{
	Use:   "trigger",
	Short: "Trigger commands",
	Long: getCommandHelp(`The trigger subcommands are used to manage triggers.

Triggers are explained at https://docs.confighub.com/background/entities/trigger/.`, ""),
	PersistentPreRunE: spacePreRunE,
}

func init() {
	addSpaceFlags(triggerCmd)
	rootCmd.AddCommand(triggerCmd)
	addExplainCmd(triggerCmd, "Trigger")
}

// buildWhereClauseFromTriggers generates a WHERE clause from trigger identifiers
func buildWhereClauseFromTriggers(triggerIds []string) (string, error) {
	return buildWhereClauseFromIdentifiers(triggerIds, "TriggerID", "Slug")
}
