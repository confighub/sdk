// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/spf13/cobra"
)

var attributeCmd = &cobra.Command{
	Use:   "attribute",
	Short: "Attribute commands",
	Long: getCommandHelp(`The attribute subcommands are used to manage attributes.

Attributes define dynamic configuration properties that register getter and setter functions
in a Space's function executor. They enable per-Space customization of the function registry
by specifying paths within resource types that can be read and written using generated
get-<slug> and set-<slug> functions.`, ""),
	PersistentPreRunE: spacePreRunE,
}

var attributeDescription string

func init() {
	addSpaceFlags(attributeCmd)
	rootCmd.AddCommand(attributeCmd)
	addExplainCmd(attributeCmd, "Attribute")
}

// buildWhereClauseFromAttributes generates a WHERE clause from attribute identifiers
func buildWhereClauseFromAttributes(attributeIds []string) (string, error) {
	return buildWhereClauseFromIdentifiers(attributeIds, "AttributeID", "Slug")
}
