// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"os"
	"strings"

	"github.com/spf13/cobra"
)

var workerEnvsCmd = &cobra.Command{
	Use:   "get-envs <worker-slug>",
	Args:  cobra.ExactArgs(1),
	Short: "Get Bridge Worker environment variables",
	Long: `Get Bridge Worker environment variables.

# Get the environment variables for a Bridge Worker
cub worker get-envs <worker-slug>`,
	RunE: workerEnvsCmdRun,
}

var workerEnvsArgs struct {
	slug string
}

func init() {
	workerCmd.AddCommand(workerEnvsCmd)
}

func workerEnvsCmdRun(_ *cobra.Command, args []string) error {
	workerEnvsArgs.slug = args[0]
	worker, err := apiGetBridgeWorkerFromSlug(workerEnvsArgs.slug)
	if err != nil {
		return err
	}

	// Detect shell from SHELL environment variable
	shell := os.Getenv("SHELL")

	tprint("# Source these environment variables with:")
	tprint("# eval \"$(cub worker get-envs %s)\"", workerEnvsArgs.slug)

	switch {
	case strings.HasSuffix(shell, "fish"):
		tprint("set -gx CONFIGHUB_WORKER_ID %s", worker.BridgeWorkerID.String())
		tprint("set -gx CONFIGHUB_WORKER_SECRET %s", worker.Secret)
	case strings.HasSuffix(shell, "csh"), strings.HasSuffix(shell, "tcsh"):
		tprint("setenv CONFIGHUB_WORKER_ID %s", worker.BridgeWorkerID.String())
		tprint("setenv CONFIGHUB_WORKER_SECRET %s", worker.Secret)
	default: // sh, bash, zsh, etc
		tprint("export CONFIGHUB_WORKER_ID=%s", worker.BridgeWorkerID.String())
		tprint("export CONFIGHUB_WORKER_SECRET=%s", worker.Secret)
	}
	return nil
}
