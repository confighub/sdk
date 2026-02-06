// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/spf13/cobra"
)

var filterCmd = &cobra.Command{
	Use:   "filter",
	Short: "Filter commands",
	Long: getCommandHelp(`The filter subcommands are used to manage filters.

Filters are explained at https://docs.confighub.com/background/entities/filter/.`, ""),
	PersistentPreRunE: spacePreRunE,
}

func init() {
	addSpaceFlags(filterCmd)
	rootCmd.AddCommand(filterCmd)
}

// buildWhereClauseFromFilters generates a WHERE clause from filter identifiers
func buildWhereClauseFromFilters(filterIds []string) (string, error) {
	return buildWhereClauseFromIdentifiers(filterIds, "FilterID", "Slug")
}
