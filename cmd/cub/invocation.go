// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/spf13/cobra"
)

var invocationCmd = &cobra.Command{
	Use:   "invocation",
	Short: "Invocation commands",
	Long: getCommandHelp(`The invocation subcommands are used to manage invocations.

Invocations are explained at https://docs.confighub.com/background/entities/invocation/.`, ""),
	PersistentPreRunE: spacePreRunE,
}

func init() {
	addSpaceFlags(invocationCmd)
	rootCmd.AddCommand(invocationCmd)
	addExplainCmd(invocationCmd, "Invocation")
}

// buildWhereClauseFromInvocations generates a WHERE clause from invocation identifiers
func buildWhereClauseFromInvocations(invocationIds []string) (string, error) {
	return buildWhereClauseFromIdentifiers(invocationIds, "InvocationID", "Slug")
}
