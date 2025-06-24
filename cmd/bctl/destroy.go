// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import "github.com/spf13/cobra"

var destroyCmd = &cobra.Command{
	Use:   "destroy",
	Short: "Trigger a Destroy operation",
	RunE:  destroyRunE,
}

func init() {
	rootCmd.AddCommand(destroyCmd)
}

func destroyRunE(cmd *cobra.Command, args []string) error {
	return executeOperation("Destroy", cmd, args)
}
