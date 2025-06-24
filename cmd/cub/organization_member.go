// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/spf13/cobra"
)

var organizationMemberCmd = &cobra.Command{
	Use:               "organization-member",
	Short:             "Organization Member commands",
	Long:              `The organization-member subcommands are used to manage organization members`,
	PersistentPreRunE: organizationPreRunE,
}

func init() {
	rootCmd.AddCommand(organizationMemberCmd)
}
