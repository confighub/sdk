// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/google/uuid"
	"github.com/spf13/cobra"

	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
)

var workerListStatusCmd = &cobra.Command{
	Use:   "list-status <worker-slug>",
	Args:  cobra.ExactArgs(1),
	Short: "List worker statuses",
	Long: `List statuses for a worker.

# List all statuses for a worker
cub worker list-status <worker-id>`,
	RunE: workerListStatusCmdRun,
}

var workerListStatusArgs struct {
	slug string
}

func init() {
	addStandardListFlags(workerListStatusCmd)
	workerCmd.AddCommand(workerListStatusCmd)
}

func workerListStatusCmdRun(_ *cobra.Command, args []string) error {
	entity, err := apiGetBridgeWorkerFromSlug(args[0])
	if err != nil {
		return err
	}

	statusRes, err := cubClientNew.ListBridgeWorkerStatusesWithResponse(ctx, uuid.MustParse(selectedSpaceID), entity.BridgeWorkerID)
	if IsAPIError(err, statusRes) {
		return InterpretErrorGeneric(err, statusRes)
	}

	statuses := make([]*goclientnew.BridgeWorkerStatus, 0, len(*statusRes.JSON200))
	for _, status := range *statusRes.JSON200 {
		statuses = append(statuses, &status)
	}
	displayListResults(statuses, getWorkerStatusSlug, displayWorkerStatusList)
	return nil
}

func getWorkerStatusSlug(status *goclientnew.BridgeWorkerStatus) string {
	return status.BridgeWorkerSlug
}

func displayWorkerStatusList(statuses []*goclientnew.BridgeWorkerStatus) {
	table := tableView()
	if !noheader {
		table.SetHeader([]string{"ID", "Status", "Remote IP", "Seen At"})
	}
	for _, status := range statuses {
		table.Append([]string{
			status.BridgeWorkerStatusID.String(),
			status.Status,
			status.IPAddress,
			status.SeenAt.String(),
		})
	}
	table.Render()
}
