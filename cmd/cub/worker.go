// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/spf13/cobra"
)

var workerCmd = &cobra.Command{
	Use:   "worker",
	Short: "Manage workers",
	Long: getCommandHelp(`Manage workers.

Workers are deprecated. Use a service account (cub serviceaccount) as the identity for
automation, including a GitOps tool that pulls Releases, and a Channel (cub channel) to consume
events and receive commands. Existing workers keep working, and a worker is still needed to
serve external functions.

Workers are explained at https://docs.confighub.com/background/entities/worker/.
A guide for how to use workers is at https://docs.confighub.com/guide/workers/.`, ""),
	PersistentPreRunE: spacePreRunE,
}

func init() {
	addSpaceFlags(workerCmd)
	rootCmd.AddCommand(workerCmd)
	addExplainCmd(workerCmd, "BridgeWorker")
}
