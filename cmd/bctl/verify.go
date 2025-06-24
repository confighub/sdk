// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/spf13/cobra"
)

var verifyCmd = &cobra.Command{
	Use:   "verify",
	Short: "Verify various resources",
	Long:  `Verify command is used to verify various resources and configurations`,
}

func init() {
	rootCmd.AddCommand(verifyCmd)
}
