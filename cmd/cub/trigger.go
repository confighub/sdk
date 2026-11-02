// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/confighub/sdk/core/constants"
	"github.com/confighub/sdk/core/cubapi"
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

// triggerPacks are the --pack values: the packs whose Triggers a list or bulk
// operation selects.
var triggerPacks []string

func addTriggerPackFlag(cmd *cobra.Command) {
	cmd.Flags().StringSliceVar(&triggerPacks, "pack", nil, "select the triggers in this pack, those labeled "+constants.PackLabel+" with the value (can be repeated or comma-separated); combined with --where using AND")
}

// applyTriggerPackFlag folds --pack into --where as a Labels.Pack IN (...)
// clause, so that everything downstream of --where selects by pack too.
func applyTriggerPackFlag() error {
	w := cubapi.NewWhere(where).InStrings("Labels."+constants.PackLabel, triggerPacks)
	if err := w.Err(); err != nil {
		return err
	}
	where = w.String()
	return nil
}
