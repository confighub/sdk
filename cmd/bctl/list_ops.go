// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import "github.com/spf13/cobra"

var listOperationsCmd = &cobra.Command{
	Use:   "operations",
	Short: "List operations",
	Run: func(cmd *cobra.Command, args []string) {
		// TODO: Implement list operations logic
	},
}

func init() {
	listCmd.AddCommand(listOperationsCmd)
}
