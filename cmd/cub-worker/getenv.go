// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var getEnvIncludeSecret bool

var getEnvCmd = &cobra.Command{
	Use:   "get-env",
	Short: "Print the loaded worker configuration",
	Long: `Print the loaded worker configuration (rootArgs) as JSON.

Values reflect the environment variables and flags processed at startup,
including defaults applied during normalization.

CONFIGHUB_WORKER_SECRET is redacted by default; pass --include-secret to
print the actual value.`,
	Args: cobra.NoArgs,
	// Skip CONFIGHUB_WORKER_ID / CONFIGHUB_WORKER_SECRET validation so this
	// command can inspect a partially-configured environment.
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error { return nil },
	RunE: func(cmd *cobra.Command, args []string) error {
		out := rootArgs
		if !getEnvIncludeSecret && out.WorkerSecret != "" {
			out.WorkerSecret = "***REDACTED***"
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(out); err != nil {
			return fmt.Errorf("encode rootArgs: %w", err)
		}
		return nil
	},
}

func init() {
	getEnvCmd.Flags().BoolVar(&getEnvIncludeSecret, "include-secret", false, "Print CONFIGHUB_WORKER_SECRET in plaintext instead of redacting it")
	rootCmd.AddCommand(getEnvCmd)
}
