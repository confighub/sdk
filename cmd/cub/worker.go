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

Workers are explained at https://docs.confighub.com/background/entities/worker/.
A guide for how to use workers is at https://docs.confighub.com/guide/workers/.`, ""),
	PersistentPreRunE: spacePreRunE,
}

func init() {
	addSpaceFlags(workerCmd)
	rootCmd.AddCommand(workerCmd)
	addExplainCmd(workerCmd, "BridgeWorker")
}
