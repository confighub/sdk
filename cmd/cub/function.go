// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/spf13/cobra"
)

var functionCmd = &cobra.Command{
	Use:               "function",
	Short:             "Function commands",
	Long:              getFunctionCommandGroupHelp(),
	PersistentPreRunE: spacePreRunE,
}

func getFunctionCommandGroupHelp() string {
	baseHelp := `The function subcommands are used to discover and execute functions`
	agentContext := `Functions operate on configuration data stored in ConfigHub without requiring local file retrieval.

Key workflow for agents:
1. Use 'function list' to discover available functions
2. Use 'function explain FUNCTION_NAME' to understand function parameters
3. Use 'function do' to execute functions on units

Functions are categorized as:
- Inspection (read-only): get-*, yq, get-placeholders
- Modification (mutating): set-*, search-replace  
- Validation (checking): no-placeholders, cel-validate, is-approved

Functions are toolchain-specific (Kubernetes/YAML, OpenTofu/HCL, etc.) and operate on units matching specified criteria.`

	return getCommandHelp(baseHelp, agentContext)
}

func init() {
	addSpaceFlags(functionCmd)
	rootCmd.AddCommand(functionCmd)
}
