// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/spf13/cobra"
)

var releaseCmd = &cobra.Command{
	Use:   "release",
	Short: "Release commands",
	Long: getCommandHelp(`The release subcommands publish, list, get, and withdraw releases.

A release bundles the configuration of the Units in a Space that are assigned to a
given Target, captured at a point in time, and serves it as an immutable OCI
bundle. Releases are created with `+"`cub release publish`"+` and removed with
`+"`cub release withdraw`"+`; their bundled content is read-only thereafter.`, ""),
	PersistentPreRunE: spacePreRunE,
}

func init() {
	addSpaceFlags(releaseCmd)
	rootCmd.AddCommand(releaseCmd)
	addExplainCmd(releaseCmd, "Release")
}
