// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

var (
	BuildTag  = "unknown"
	BuildDate = "unknown"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Show version, commit and build date",
	Long:  `Show the build version, commit hash, and build date for cub`,
	Run:   versionCmdRun,
}

func init() {
	rootCmd.AddCommand(versionCmd)
}

func versionCmdRun(cmd *cobra.Command, args []string) {
	fmt.Printf("Client Version:\n")
	fmt.Printf("  Commit:     %s\n", BuildTag)
	fmt.Printf("  Build Date: %s\n", BuildDate)
	apiInfo := GetApiInfo()
	fmt.Printf("Server Version:\n")
	fmt.Printf("  URL:        %s\n", cubContext.ConfigHubURL)
	fmt.Printf("  Commit:     %s\n", apiInfo.Build)
}
