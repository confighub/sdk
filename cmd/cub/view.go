// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/spf13/cobra"
)

var viewCmd = &cobra.Command{
	Use:               "view",
	Short:             "View commands",
	Long:              `The view subcommands are used to manage views`,
	PersistentPreRunE: spacePreRunE,
}

func init() {
	addSpaceFlags(viewCmd)
	rootCmd.AddCommand(viewCmd)
}

// buildWhereClauseFromViews generates a WHERE clause from view identifiers
func buildWhereClauseFromViews(viewIds []string) (string, error) {
	return buildWhereClauseFromIdentifiers(viewIds, "ViewID", "Slug")
}