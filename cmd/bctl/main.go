// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:               "bctl",
	Short:             "Bridge control CLI tool",
	Long:              `Command line interface for managing bridge operations and configurations`,
	SilenceErrors:     true,
	SilenceUsage:      true,
	PersistentPreRunE: rootPreRunE,
}

var rootArgs struct {
	verbose      bool
	configHubURL string
	workerID     string
	workerSecret string
}

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func init() {
	// Persistent flags are available to all subcommands
	rootCmd.PersistentFlags().BoolVarP(&rootArgs.verbose, "verbose", "v", false, "Enable verbose logging")
	rootCmd.PersistentFlags().StringVarP(&rootArgs.configHubURL, "url", "u", getEnvOrDefault("CONFIGHUB_URL", "https://localhost:9443"), "ConfigHub server URL")
	rootCmd.PersistentFlags().StringVarP(&rootArgs.workerID, "worker-id", "w", os.Getenv("CONFIGHUB_WORKER_ID"), "Worker ID")
	rootCmd.PersistentFlags().StringVarP(&rootArgs.workerSecret, "worker-secret", "s", os.Getenv("CONFIGHUB_WORKER_SECRET"), "Worker Secret")
}

func rootPreRunE(cmd *cobra.Command, args []string) error {
	// skip value checks if the command does not interact with the server
	if cmd == generateSecretCmd {
		return nil
	}

	if rootArgs.configHubURL == "" {
		return fmt.Errorf("configHub URL is required. Set via --url flag or CONFIGHUB_URL environment variable")
	}

	if rootArgs.workerID == "" {
		return fmt.Errorf("worker ID is required. Set via --worker-id flag or CONFIGHUB_WORKER_ID environment variable")
	}

	if rootArgs.workerSecret == "" {
		return fmt.Errorf("worker secret is required. Set via --worker-secret flag or CONFIGHUB_WORKER_SECRET environment variable")
	}

	return nil
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
