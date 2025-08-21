// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/spf13/cobra"
)

var filterCmd = &cobra.Command{
	Use:               "filter",
	Short:             "Filter commands",
	Long:              `The filter subcommands are used to manage filters`,
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