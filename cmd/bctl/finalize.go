// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import "github.com/spf13/cobra"

var finalizeCmd = &cobra.Command{
	Use:   "finalize",
	Short: "Trigger a Finalize operation for the worker life cycle",
	RunE:  finalizeRunE,
}

func init() {
	rootCmd.AddCommand(finalizeCmd)
}

func finalizeRunE(cmd *cobra.Command, args []string) error {
	return executeOperation("Finalize", cmd, args)
}
