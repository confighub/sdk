// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"os"

	"github.com/spf13/cobra"
)

var datasetCmdGroup = &cobra.Command{
	Use:   "dataset <command>",
	Short: "dataset commands",
	Long:  `dataset commands`,
}

func init() {
	if os.Getenv("CONFIGHUB_EXPERIMENTAL") != "" {
		rootCmd.AddCommand(datasetCmdGroup)
	}
}
