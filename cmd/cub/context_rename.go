// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

var contextRenameCmd = &cobra.Command{
	Use:   "rename [old-name] [new-name]",
	Short: "Rename a context",
	Long: getCommandHelp(`Rename an existing context to a new name.

Examples:
`+"```"+`
  # Rename a context
  cub context rename old-name new-name

  # Rename the current context
  cub context rename staging production
`+"```"+`
`, ""),
	Args: cobra.ExactArgs(2),
	RunE: contextRenameCmdRun,
}

func init() {
	contextCmd.AddCommand(contextRenameCmd)
}

func contextRenameCmdRun(_ *cobra.Command, args []string) error {
	if contextManager == nil {
		return fmt.Errorf("context manager not initialized")
	}

	oldName := args[0]
	newName := args[1]

	// Rename the context
	if err := contextManager.RenameContext(oldName, newName); err != nil {
		return err
	}

	err := contextManager.SaveConfig()
	if err != nil {
		return fmt.Errorf("failed to save context: %w", err)
	}

	tprint("Successfully renamed context %q to %q", oldName, newName)

	// If this was the current context, remind the user
	if contextManager.CurrentContextName() == newName {
		fmt.Printf("Note: %q is now the current context\n", newName)
	}

	return nil
}
