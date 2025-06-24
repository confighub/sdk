// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var workerListCmd = &cobra.Command{
	Use:   "list",
	Short: "List workers",
	Long:  `List workers`,
	RunE:  workerListCmdRun,
}

func init() {
	addStandardListFlags(workerListCmd)
	workerCmd.AddCommand(workerListCmd)
}

func workerListCmdRun(_ *cobra.Command, _ []string) error {
	workers, err := apiListBridgeworkers(selectedSpaceID)
	if err != nil {
		return err
	}

	displayListResults(workers, getWorkerSlug, displayWorkerList)
	return nil
}

func apiListBridgeworkers(spaceID string) ([]*goclientnew.BridgeWorker, error) {
	workersRes, err := cubClientNew.ListBridgeWorkersWithResponse(ctx, uuid.MustParse(spaceID), nil)
	if IsAPIError(err, workersRes) {
		return nil, InterpretErrorGeneric(err, workersRes)
	}

	workers := make([]*goclientnew.BridgeWorker, 0, len(*workersRes.JSON200))
	for _, worker := range *workersRes.JSON200 {
		workers = append(workers, worker.BridgeWorker)
	}
	return workers, nil
}

func getWorkerSlug(worker *goclientnew.BridgeWorker) string {
	return worker.Slug
}

func displayWorkerList(workers []*goclientnew.BridgeWorker) {
	table := tableView()
	if !noheader {
		table.SetHeader([]string{"Display-Name", "Slug", "ID", "Condition"})
	}
	for _, worker := range workers {
		table.Append([]string{
			worker.DisplayName,
			worker.Slug,
			worker.BridgeWorkerID.String(),
			worker.Condition,
		})
	}
	table.Render()
}
