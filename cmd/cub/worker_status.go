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

var workerStatusCmd = &cobra.Command{
	Use:           "status [<worker-slug>]",
	Short:         "Check status of background worker(s)",
	Long:          getCommandHelp(`Check if a worker is running in the background. If no worker slug is provided, shows status of all workers.`, ""),
	Args:          cobra.MaximumNArgs(1),
	RunE:          workerStatusCmdRun,
	SilenceUsage:  true,
	SilenceErrors: true,
}

func init() {
	workerCmd.AddCommand(workerStatusCmd)
}

func workerStatusCmdRun(cmd *cobra.Command, args []string) error {
	confighubDir := filepath.Join(os.Getenv("HOME"), CONFIGHUB_DIR)

	if len(args) == 1 {
		// Check specific worker
		return checkWorkerStatus(confighubDir, args[0])
	}

	// Check all workers
	return checkAllWorkersStatus(confighubDir)
}

func checkWorkerStatus(confighubDir, workerSlug string) error {
	pidFile := filepath.Join(confighubDir, "worker", "pid", fmt.Sprintf("%s.pid", workerSlug))
	logFile := filepath.Join(confighubDir, "worker", "log", fmt.Sprintf("%s.log", workerSlug))

	// Read PID from file
	pidBytes, err := os.ReadFile(pidFile)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Printf("Worker '%s' is not running\n", workerSlug)
			return nil
		}
		return fmt.Errorf("failed to read PID file: %w", err)
	}

	pid, err := strconv.Atoi(string(pidBytes))
	if err != nil {
		return fmt.Errorf("invalid PID in file: %w", err)
	}

	// Check if process exists
	process, err := os.FindProcess(pid)
	if err != nil {
		fmt.Printf("Worker '%s' is not running (process not found)\n", workerSlug)
		return nil
	}

	// Send signal 0 to check if process is alive
	err = process.Signal(syscall.Signal(0))
	if err != nil {
		fmt.Printf("Worker '%s' is not running (PID %d does not exist)\n", workerSlug, pid)
		// Clean up stale PID file
		os.Remove(pidFile)
		return nil
	}

	fmt.Printf("Worker '%s' is running (PID: %d)\n", workerSlug, pid)
	fmt.Printf("Log file: %s\n", logFile)
	fmt.Printf("PID file: %s\n", pidFile)

	return nil
}

func checkAllWorkersStatus(confighubDir string) error {
	// Find all PID files
	pidFiles, err := filepath.Glob(filepath.Join(confighubDir, "worker", "pid", "*.pid"))
	if err != nil {
		return fmt.Errorf("failed to list PID files: %w", err)
	}

	if len(pidFiles) == 0 {
		fmt.Println("No background workers running")
		return nil
	}

	fmt.Println("Background workers:")
	for _, pidFile := range pidFiles {
		// Extract worker slug from filename
		base := filepath.Base(pidFile)
		workerSlug := base[:len(base)-len(".pid")]

		// Read PID
		pidBytes, err := os.ReadFile(pidFile)
		if err != nil {
			fmt.Printf("  - %s: error reading PID file\n", workerSlug)
			continue
		}

		pid, err := strconv.Atoi(string(pidBytes))
		if err != nil {
			fmt.Printf("  - %s: invalid PID\n", workerSlug)
			continue
		}

		// Check if process exists
		process, err := os.FindProcess(pid)
		if err != nil || process.Signal(syscall.Signal(0)) != nil {
			fmt.Printf("  - %s: not running (stale PID file)\n", workerSlug)
			os.Remove(pidFile)
			continue
		}

		fmt.Printf("  - %s: running (PID: %d)\n", workerSlug, pid)
	}

	return nil
}
