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
	Long: getCommandHelp(`Get Bridge Worker environment variables.

Examples:
`+"```"+`
  # Get the environment variables for a Bridge Worker
  cub worker get-envs <worker-slug>

  # Emit plain KEY=value lines (no shell prefix, no comments). Suitable for
  # writing to a .env file or feeding to a kustomize secretGenerator.
  cub worker get-envs --no-export <worker-slug>
`+"```"+`
`, ""),
	RunE: workerEnvsCmdRun,
}

var workerEnvsArgs struct {
	slug     string
	noExport bool
}

func init() {
	workerEnvsCmd.Flags().BoolVar(&workerEnvsArgs.noExport, "no-export", false, "Emit plain KEY=value lines without shell-specific export/setenv prefixes or source-with comments")
	workerCmd.AddCommand(workerEnvsCmd)
}

func workerEnvsCmdRun(_ *cobra.Command, args []string) error {
	workerEnvsArgs.slug = args[0]
	worker, err := apiGetBridgeWorkerFromSlug(workerEnvsArgs.slug, "*") // get all fields for now
	if err != nil {
		return err
	}

	if workerEnvsArgs.noExport {
		tprint("CONFIGHUB_WORKER_ID=%s", worker.BridgeWorkerID.String())
		tprint("CONFIGHUB_WORKER_SECRET=%s", worker.Secret)
		return nil
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
