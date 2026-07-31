// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/spf13/cobra"
)

var resourceCmd = &cobra.Command{
	Use:   "resource",
	Short: "Resource commands",
	Long: getCommandHelp(`The resource subcommands are used to inspect the individual resources
(Kubernetes resources, infrastructure resources, application configuration files, ...) contained
in the configuration data of your units.

Resources are derived from unit data: ConfigHub extracts them whenever a unit changes, so they are
read-only and there is no command to create, update, or delete one. Change the unit instead.`, ""),
	PersistentPreRunE: spacePreRunE,
}

func init() {
	addSpaceFlags(resourceCmd)
	rootCmd.AddCommand(resourceCmd)
	addExplainCmd(resourceCmd, "Resource")
}
