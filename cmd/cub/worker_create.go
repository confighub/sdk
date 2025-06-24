// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var workerCreateCmd = &cobra.Command{
	Use:   "create <worker-slug>",
	Args:  cobra.ExactArgs(1),
	Short: "Create a worker",
	Long: `Create a bridge worker in your environment. Workers are responsible for executing tasks and managing resources in your infrastructure.

The worker-slug must be unique within a space. Workers can be used to:
  1. Apply configurations to target environments
  2. Monitor and manage resource states

Examples:
  # Create a worker in a space
  cub worker create --space my-space k8s-worker-1

  # Create a worker and run it for the Kubernetes toolchain
  cub worker create --space my-space worker-1
  cub worker run --space my-space worker-1 -t=kubernetes`,
	RunE: workerCreateCmdRun,
}

var workerCreateArgs struct {
	slug string
}

func init() {
	addStandardCreateFlags(workerCreateCmd)
	workerCmd.AddCommand(workerCreateCmd)
}

func workerCreateCmdRun(cmd *cobra.Command, args []string) error {
	workerDetails := &goclientnew.BridgeWorker{}
	if flagPopulateModelFromStdin {
		if err := populateNewModelFromStdin(workerDetails); err != nil {
			return err
		}
	}
	err := setLabels(&workerDetails.Labels)
	if err != nil {
		return err
	}
	workerDetails.Slug = makeSlug(args[0])
	workerDetails.DisplayName = args[0]
	workerDetails.SpaceID = uuid.MustParse(selectedSpaceID)

	workerDetails, err = apiCreateWorker(workerDetails, workerDetails.SpaceID)
	if err != nil {
		return err
	}
	displayCreateResults(workerDetails, "bridgeworker", args[0], workerDetails.BridgeWorkerID.String(), displayWorkerDetails)
	return nil
}

func apiCreateWorker(details *goclientnew.BridgeWorker, spaceID uuid.UUID) (*goclientnew.BridgeWorker, error) {
	workerRes, err := cubClientNew.CreateBridgeWorkerWithResponse(ctx, spaceID, *details)
	if IsAPIError(err, workerRes) {
		return nil, InterpretErrorGeneric(err, workerRes)
	}

	return workerRes.JSON200, nil
}

func displayWorkerDetails(workerDetails *goclientnew.BridgeWorker) {
	// TODO
}
