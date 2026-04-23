// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

// addToolchainToWhereClause AND's a ToolchainType equality constraint into an existing
// where clause. Mirrors addSpaceIDToWhereClause. Empty toolchain returns the clause
// unchanged so --toolchain can be left unset.
func addToolchainToWhereClause(whereClause, toolchain string) string {
	if toolchain == "" {
		return whereClause
	}
	constraint := fmt.Sprintf("ToolchainType = '%s'", toolchain)
	if whereClause == "" {
		return constraint
	}
	return fmt.Sprintf("%s AND %s", whereClause, constraint)
}

var functionCmd = &cobra.Command{
	Use:               "function",
	Short:             "Function commands",
	Long:              getFunctionCommandGroupHelp(),
	PersistentPreRunE: spacePreRunE,
}

func getFunctionCommandGroupHelp() string {
	baseHelp := `The function subcommands are used to discover and execute functions.

Functions are explained at https://docs.confighub.com/background/entities/function/.
A guide for how to use functions is at https://docs.confighub.com/guide/functions/.`
	agentContext := `Functions operate on configuration data stored in ConfigHub without requiring local file retrieval.

Key workflow for agents:
1. Use 'function list' to discover available functions
2. Use 'function explain FUNCTION_NAME' to understand function parameters
3. Use 'function do' to execute functions on units

Functions are categorized as:
- Inspection (read-only): get-*, yq, get-placeholders
- Modification (mutating): set-*, search-replace  
- Validation (checking): vet-placeholders, vet-celexpr, vet-approvedby

Functions are toolchain-specific (Kubernetes/YAML, OpenTofu/HCL, etc.) and operate on units matching specified criteria.`

	return getCommandHelp(baseHelp, agentContext)
}

var executorSpace string

func init() {
	addSpaceFlags(functionCmd)
	functionCmd.PersistentFlags().StringVar(&executorSpace, "executor-space", "", "Space ID or slug whose executor to use for builtin functions (org-level only)")
	rootCmd.AddCommand(functionCmd)
}
