// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"

	"github.com/confighub/sdk/core/cubapi"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var workerSecretCmd = &cobra.Command{
	Use:   "get-secret <worker-slug>",
	Args:  cobra.ExactArgs(1),
	Short: "Get worker secrets",
	Long: getCommandHelp(`Get worker secrets.

Examples:
`+"```"+`
  # Get the secret of a worker
  cub worker get-secret <worker-slug>
`+"```"+`
`, ""),
	RunE: workerSecretCmdRun,
}

var workerSecretArgs struct {
	slug string
}

func init() {
	workerCmd.AddCommand(workerSecretCmd)
}

func workerSecretCmdRun(_ *cobra.Command, args []string) error {
	entity, err := apiGetBridgeWorkerFromSlug(args[0], "*") // get all fields for now
	if err != nil {
		return err
	}
	workerRes, err := cubClientNew.GetBridgeWorkerWithResponse(ctx, uuid.MustParse(selectedSpaceID),
		entity.BridgeWorkerID, nil)
	if cubapi.IsAPIError(err, workerRes) {
		return cubapi.InterpretErrorGeneric(err, workerRes)
	}

	if workerRes.JSON200 == nil || workerRes.JSON200.BridgeWorker == nil {
		return fmt.Errorf("failed to get worker secret")
	}
	tprint("%s", workerRes.JSON200.BridgeWorker.Secret)
	return nil
}
