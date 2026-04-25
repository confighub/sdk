// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"os"

	"github.com/spf13/cobra"
)

var docgenCmd = &cobra.Command{
	Use:   "docgen",
	Short: "Generate documentation for the worker",
	Long: `Generate structured documentation for the worker.

Subcommands:
  env      - print the worker's environment variables as a JSON Schema
  command  - print Cobra YAML documentation for the worker command (flags, subcommands, description)`,
	// Override the root's PersistentPreRunE so docgen subcommands skip
	// CONFIGHUB_WORKER_ID / CONFIGHUB_WORKER_SECRET validation.
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error { return nil },
}

var docgenEnvCmd = &cobra.Command{
	Use:   "env",
	Short: "Print worker environment variables as a JSON Schema",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return printEnvSchema(os.Stdout)
	},
}

var docgenCommandCmd = &cobra.Command{
	Use:   "command",
	Short: "Print Cobra YAML documentation for the worker command",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return printCommandDocs(cmd.Root(), os.Stdout)
	},
}

func init() {
	docgenCmd.AddCommand(docgenEnvCmd)
	docgenCmd.AddCommand(docgenCommandCmd)
	rootCmd.AddCommand(docgenCmd)
}
