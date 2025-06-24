// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/spf13/cobra"
)

var applyCmd = &cobra.Command{
	Use:   "apply",
	Short: "Trigger an Apply operation",
	RunE:  applyRunE,
}

func init() {
	rootCmd.AddCommand(applyCmd)
}

func applyRunE(cmd *cobra.Command, args []string) error {
	return executeOperation("Apply", cmd, args)
}
