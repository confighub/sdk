// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var workerRunCmd = &cobra.Command{
	Use:           "run",
	Args:          cobra.ExactArgs(1),
	RunE:          workerRunCmdRun,
	SilenceUsage:  true,
	SilenceErrors: true,
}

var workerRunArgs struct {
	workerType        string
	envs              []string
	enableMultiplexer bool
}

func init() {
	workerRunCmd.Flags().StringVarP(&workerRunArgs.workerType, "worker-type", "t", "kubernetes", "worker type")
	workerRunCmd.Flags().StringSliceVarP(&workerRunArgs.envs, "env", "e", []string{}, "environment variables")
	workerRunCmd.Flags().BoolVar(&workerRunArgs.enableMultiplexer, "enable-multiplexer", false, "Enable multiplexer mode with prefixes and multi-worker support")

	// [jj]: I commented this out and set "kubernetes" as default type.
	// TODO: Type should not be required at all.
	// if err := workerRunCmd.MarkFlagRequired("worker-type"); err != nil {
	// 	panic(err)
	// }
	workerCmd.AddCommand(workerRunCmd)
}

func workerRunCmdRun(cmd *cobra.Command, args []string) error {
	// Auto-enable multiplexer if worker type contains comma
	if strings.Contains(workerRunArgs.workerType, ",") && !cmd.Flags().Changed("enable-multiplexer") {
		workerRunArgs.enableMultiplexer = true
	}

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

	// Binary must be placed in ~/.confighub/bin/cub-worker-run unless specified in env var
	workerExecutable := os.Getenv("CONFIGHUB_WORKER_EXECUTABLE")
	if workerExecutable == "" {
		workerExecutable = filepath.Join(os.Getenv("HOME"), CONFIGHUB_DIR, "bin", "cub-worker-run")
	}

	// Check if binary exists in same directory
	if _, err := os.Stat(workerExecutable); os.IsNotExist(err) {
		if err != nil {
			return err
		}
	}

	// Build command args
	cmdArgs := []string{workerRunArgs.workerType}
	if workerRunArgs.enableMultiplexer {
		cmdArgs = append(cmdArgs, "--enable-multiplexer")
	}

	workerCommand := exec.Command(workerExecutable, cmdArgs...)
	workerCommand.Stdin = os.Stdin
	workerCommand.Stdout = os.Stdout
	workerCommand.Stderr = os.Stderr
	workerCommand.Env = append(os.Environ(),
		"CONFIGHUB_WORKER_ID="+worker.BridgeWorkerID.String(),
		"CONFIGHUB_WORKER_SECRET="+worker.Secret)
	// Also append -e to envs
	// TODO redesign this by adding a prefix for example REPO would become WORKER_TARGET_REPO
	workerCommand.Env = append(workerCommand.Env, workerRunArgs.envs...)

	return workerCommand.Run()
}
