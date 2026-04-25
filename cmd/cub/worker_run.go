// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var workerRunCmd = &cobra.Command{
	Use:   "run",
	Short: "Run a worker locally",
	Long: getCommandHelp(`Run a worker locally to serve one or more provider types.

Each ProviderType corresponds to one or more ToolchainTypes.
For example, the "Kubernetes" provider type corresponds to the Kubernetes/YAML ToolchainType

Some ToolchainTypes are supported by multiple ProviderTypes, and some ProviderTypes support
multiple ToolchainTypes.

The available ProviderTypes are:

- ConfigHub
- Kubernetes
- FluxRenderer
- FluxOCI
- ArgoCDRenderer
- ArgoCDOCI
- ConfigMapRenderer
- OpenTofu/AWS (experimental)

Here the provider types are case-insensitive and they can be comma-separated, like "kubernetes,configmap".
	`, ""),
	Args:          cobra.ExactArgs(1),
	RunE:          workerRunCmdRun,
	SilenceUsage:  true,
	SilenceErrors: true,
}

var workerRunArgs struct {
	workerProviderTypes string
	workerFunctions     string
	envs                []string
	executable          string
	daemon              bool
}

func init() {
	workerRunCmd.Flags().StringVarP(&workerRunArgs.workerProviderTypes, "provider-types", "t", "", "Comma-separated list of provider types")
	workerRunCmd.Flags().StringVarP(&workerRunArgs.workerFunctions, "functions", "f", "", "Comma-separated list of worker function names (e.g., vet-kyverno-server)")
	workerRunCmd.Flags().StringSliceVarP(&workerRunArgs.envs, "env", "e", []string{}, "environment variables")
	workerRunCmd.Flags().StringVar(&workerRunArgs.executable, "executable", "", "Path to worker executable (overrides CONFIGHUB_WORKER_EXECUTABLE env var)")
	workerRunCmd.Flags().BoolVarP(&workerRunArgs.daemon, "daemon", "d", false, "Run worker in background (daemon mode)")

	workerCmd.AddCommand(workerRunCmd)
}

func workerRunCmdRun(cmd *cobra.Command, args []string) error {
	spaceID := uuid.MustParse(selectedSpaceID)
	worker, err := apiGetBridgeWorkerFromSlug(args[0], "*") // get all fields for now
	if err != nil {
		// assume worker not found and create a default worker on the fly
		worker, err = apiCreateWorker(&goclientnew.BridgeWorker{
			SpaceID: spaceID,
			Slug:    args[0],
		}, spaceID)
		if err != nil {
			return err
		}
	}

	// Priority: --executable flag > CONFIGHUB_WORKER_EXECUTABLE env > default path
	workerExecutable := workerRunArgs.executable
	if workerExecutable == "" {
		workerExecutable = os.Getenv("CONFIGHUB_WORKER_EXECUTABLE")
	}
	if workerExecutable == "" {
		workerExecutable = filepath.Join(os.Getenv("HOME"), CONFIGHUB_DIR, "bin", "cub-worker-run")
	}

	// Check if binary exists in same directory
	if _, err := os.Stat(workerExecutable); os.IsNotExist(err) {
		if err != nil {
			return err
		}
	}

	// Do not use command-line arguments
	cmdArgs := []string{}

	activeContext := contextManager.ActiveContext()
	serverURL := activeContext.Coordinate.ServerURL

	workerCommand := exec.Command(workerExecutable, cmdArgs...)
	workerCommand.Stdin = os.Stdin
	workerCommand.Stdout = os.Stdout
	workerCommand.Stderr = os.Stderr
	workerCommand.Env = append(os.Environ(),
		"CONFIGHUB_URL="+serverURL,
		"CONFIGHUB_WORKER_ID="+worker.BridgeWorkerID.String(),
		"CONFIGHUB_WORKER_SECRET="+worker.Secret,
		"CONFIGHUB_WORKER_PROVIDER_TYPES="+workerRunArgs.workerProviderTypes, // may be ""
		"CONFIGHUB_WORKER_FUNCTIONS="+workerRunArgs.workerFunctions,          // may be ""
	)
	// Also append -e to envs
	// TODO redesign this by adding a prefix for example REPO would become WORKER_TARGET_REPO
	workerCommand.Env = append(workerCommand.Env, workerRunArgs.envs...)

	if workerRunArgs.daemon {
		return runWorkerInDaemonMode(workerCommand, worker.Slug)
	}

	return workerCommand.Run()
}

// runWorkerInDaemonMode starts the worker in daemon mode, redirects logs to file, and saves PID
func runWorkerInDaemonMode(cmd *exec.Cmd, workerSlug string) error {
	confighubDir := filepath.Join(os.Getenv("HOME"), CONFIGHUB_DIR)
	logsDir := filepath.Join(confighubDir, "worker", "log")
	pidsDir := filepath.Join(confighubDir, "worker", "pid")

	// Create logs and pids directories if they don't exist
	if err := os.MkdirAll(logsDir, 0755); err != nil {
		return fmt.Errorf("failed to create logs directory: %w", err)
	}
	if err := os.MkdirAll(pidsDir, 0755); err != nil {
		return fmt.Errorf("failed to create pids directory: %w", err)
	}

	// Create log file
	logFile := filepath.Join(logsDir, fmt.Sprintf("%s.log", workerSlug))
	logWriter, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("failed to create log file: %w", err)
	}
	defer logWriter.Close()

	// Redirect stdout and stderr to log file
	cmd.Stdout = logWriter
	cmd.Stderr = logWriter
	cmd.Stdin = nil // No stdin for background process

	// Start the process
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start worker: %w", err)
	}

	// Save PID to file
	pidFile := filepath.Join(pidsDir, fmt.Sprintf("%s.pid", workerSlug))
	if err := os.WriteFile(pidFile, []byte(strconv.Itoa(cmd.Process.Pid)), 0644); err != nil {
		// Process is already running, but we couldn't save PID - warn but don't fail
		fmt.Printf("Warning: worker started (PID %d) but failed to save PID file: %v\n", cmd.Process.Pid, err)
	}

	fmt.Printf("Worker '%s' started in daemon mode (PID: %d)\n", workerSlug, cmd.Process.Pid)
	fmt.Printf("Logs: %s\n", logFile)
	fmt.Printf("PID file: %s\n", pidFile)
	fmt.Printf("\nTo stop the worker, run: cub worker stop %s\n", workerSlug)

	return nil
}
