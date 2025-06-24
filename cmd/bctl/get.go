// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import "github.com/spf13/cobra"

var getCmd = &cobra.Command{
	Use:   "get",
	Short: "Trigger a Get operation to obtain outputs and live state from the infrastructure",
	RunE:  getRunE,
}

func init() {
	rootCmd.AddCommand(getCmd)
}

func getRunE(cmd *cobra.Command, args []string) error {
	return executeOperation("Get", cmd, args)
}
