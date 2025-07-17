// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/spf13/cobra"
)

var unitCmd = &cobra.Command{
	Use:               "unit",
	Short:             "Unit commands",
	Long:              getUnitCommandGroupHelp(),
	PersistentPreRunE: spacePreRunE,
}

func getUnitCommandGroupHelp() string {
	baseHelp := `The unit subcommands are used to manage units`
	agentContext := `Units are the core configuration entities in ConfigHub, containing structured data like Kubernetes YAML, properties files, or HCL configurations.

Unit lifecycle workflow:
1. Create units from configuration files ('unit create')
2. Inspect and modify units using functions ('function do')
3. Validate configuration ('function do' with validation functions)
4. Approve units for deployment ('unit approve')
5. Apply units to live infrastructure ('unit apply')

Key commands for agents:
- 'unit list' - discover existing units with filtering
- 'unit get' - retrieve unit details and configuration data
- 'unit create' - create new units from local files
- 'unit update' - modify existing units

Units are scoped to spaces and can be linked to other units to model dependencies.`
	
	return getCommandHelp(baseHelp, agentContext)
}

func init() {
	addSpaceFlags(unitCmd)
	rootCmd.AddCommand(unitCmd)
}
