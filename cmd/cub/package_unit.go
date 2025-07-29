// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

// The unit commands are used to interact with units within a package.

package main

import (
	"github.com/spf13/cobra"
)

var packageUnitCmd = &cobra.Command{
	Use:   "unit <command>",
	Short: "unit commands",
	Long:  `unit commands for packages`,
}

func init() {
	packageCmdGroup.AddCommand(packageUnitCmd)
}
