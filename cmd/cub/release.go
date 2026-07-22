// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/spf13/cobra"
)

var releaseCmd = &cobra.Command{
	Use:   "release",
	Short: "Release commands",
	Long: getCommandHelp(`The release subcommands publish, list, get, withdraw, and delete releases.

A release bundles the configuration of the Units in a Space that are assigned to a
given Target, captured at a point in time, and serves it as an immutable OCI
bundle. Releases are created with `+"`cub release publish`"+`, taken out of service
with `+"`cub release withdraw`"+`, and removed with `+"`cub release delete`"+`;
their bundled content is read-only throughout.`, ""),
	PersistentPreRunE: spacePreRunE,
}

func init() {
	addSpaceFlags(releaseCmd)
	rootCmd.AddCommand(releaseCmd)
	addExplainCmd(releaseCmd, "Release")
}
