// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"syscall"

	"github.com/spf13/cobra"
)

var workerStopCmd = &cobra.Command{
	Use:           "stop <worker-slug>",
	Short:         "Stop a background worker",
	Long:          getCommandHelp(`Stop a worker running in the background`, ""),
	Args:          cobra.ExactArgs(1),
	RunE:          workerStopCmdRun,
	SilenceUsage:  true,
	SilenceErrors: true,
}

func init() {
	workerCmd.AddCommand(workerStopCmd)
}

func workerStopCmdRun(cmd *cobra.Command, args []string) error {
	workerSlug := args[0]
	confighubDir := filepath.Join(os.Getenv("HOME"), CONFIGHUB_DIR)
	pidFile := filepath.Join(confighubDir, "worker", "pid", fmt.Sprintf("%s.pid", workerSlug))

	// Read PID from file
	pidBytes, err := os.ReadFile(pidFile)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("worker '%s' is not running (PID file not found)", workerSlug)
		}
		return fmt.Errorf("failed to read PID file: %w", err)
	}

	pid, err := strconv.Atoi(string(pidBytes))
	if err != nil {
		return fmt.Errorf("invalid PID in file: %w", err)
	}

	// Find the process
	process, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("failed to find process: %w", err)
	}

	// Try to send SIGTERM for graceful shutdown
	fmt.Printf("Stopping worker '%s' (PID: %d)...\n", workerSlug, pid)
	if err := process.Signal(syscall.SIGTERM); err != nil {
		// Process might not exist
		if err == os.ErrProcessDone || err.Error() == "os: process already finished" {
			fmt.Printf("Worker '%s' was not running\n", workerSlug)
			os.Remove(pidFile)
			return nil
		}
		return fmt.Errorf("failed to stop worker: %w", err)
	}

	// Remove PID file
	if err := os.Remove(pidFile); err != nil {
		fmt.Printf("Warning: failed to remove PID file: %v\n", err)
	}

	fmt.Printf("Worker '%s' stopped successfully\n", workerSlug)
	return nil
}
