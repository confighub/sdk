// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

var contextDeleteCmd = &cobra.Command{
	Use:   "delete [name]",
	Short: "Delete a context",
	Long: `Delete a context and its associated token file.

Examples:
  # Delete a context
  cub context delete staging
  
  # Delete a context (will fail if it's the current context)
  cub context delete prod-acme`,
	Args: cobra.ExactArgs(1),
	RunE: contextDeleteCmdRun,
}

func init() {
	contextCmd.AddCommand(contextDeleteCmd)
}

func contextDeleteCmdRun(_ *cobra.Command, args []string) error {
	if contextManager == nil {
		return fmt.Errorf("context manager not initialized")
	}

	name := args[0]

	// Delete the context
	if err := contextManager.DeleteContext(name); err != nil {
		return err
	}

	tprint("Successfully deleted context %q", name)
	
	// If there are no more contexts, inform the user
	if len(contextManager.ListContexts()) == 0 {
		fmt.Println("No contexts remaining. Use 'cub context create' to create a new context.")
	}

	return nil
}