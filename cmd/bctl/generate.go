// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/spf13/cobra"
)

var generateCmd = &cobra.Command{
	Use:   "generate",
	Short: "Generate various resources",
	Long:  `Generate command is used to generate various resources and configurations`,
}

func init() {
	rootCmd.AddCommand(generateCmd)
}
