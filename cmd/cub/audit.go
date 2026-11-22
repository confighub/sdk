// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/spf13/cobra"
)

var auditCmd = &cobra.Command{
	Use:   "audit",
	Short: "Audit event commands",
	Long:  getCommandHelp(`The audit subcommands are used to view the audit events of the organization: for each action on an entity, who made it, under what authority, to which entity, and when. Audit events are recorded by the actions they describe, and are never changed or deleted through the API.`, ""),
	// just globalPreRun
}

func init() {
	rootCmd.AddCommand(auditCmd)
	addExplainCmd(auditCmd, "AuditEvent")
}
