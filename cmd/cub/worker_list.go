// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var workerListCmd = &cobra.Command{
	Use:         "list",
	Short:       "List workers",
	Long:        `List workers in a space or across all spaces. Use --space "*" to list workers across all spaces.`,
	Annotations: map[string]string{"OrgLevel": ""},
	RunE:        workerListCmdRun,
}

// Default columns to display when no custom columns are specified
var defaultWorkerColumns = []string{"BridgeWorker.Slug", "BridgeWorker.Condition", "Space.Slug", "BridgeWorker.LastSeenAt"}

// Worker-specific aliases
var workerAliases = map[string]string{
	"Name": "BridgeWorker.Slug",
	"ID":   "BridgeWorker.BridgeWorkerID",
}

// Worker custom column dependencies
var workerCustomColumnDependencies = map[string][]string{}

func init() {
	addStandardListFlags(workerListCmd)
	workerCmd.AddCommand(workerListCmd)
}

func workerListCmdRun(_ *cobra.Command, _ []string) error {
	var workers []*goclientnew.ExtendedBridgeWorker
	var err error
	if selectedSpaceID == "*" {
		// Cross-space listing
		workers, err = apiListAllBridgeWorkers(where, selectFields)
		if err != nil {
			return err
		}
	} else {
		// Single space listing
		workers, err = apiListBridgeworkers(selectedSpaceID, where, selectFields)
		if err != nil {
			return err
		}
	}
	displayListResults(workers, getExtendedWorkerSlug, displayExtendedWorkerList)
	return nil
}

func apiListBridgeworkers(spaceID string, whereFilter string, selectParam string) ([]*goclientnew.ExtendedBridgeWorker, error) {
	newParams := &goclientnew.ListBridgeWorkersParams{}
	if whereFilter != "" {
		newParams.Where = &whereFilter
	}
	if contains != "" {
		newParams.Contains = &contains
	}
	include := "SpaceID"
	newParams.Include = &include
	// Handle select parameter
	selectValue := handleSelectParameter(selectParam, selectFields, func() string {
		baseFields := []string{"Slug", "BridgeWorkerID", "SpaceID", "OrganizationID", "Secret"}
		return buildSelectList("BridgeWorker", "", include, defaultWorkerColumns, workerAliases, workerCustomColumnDependencies, baseFields)
	})
	if selectValue != "" && selectValue != "*" {
		newParams.Select = &selectValue
	}
	workersRes, err := cubClientNew.ListBridgeWorkersWithResponse(ctx, uuid.MustParse(spaceID), newParams)
	if IsAPIError(err, workersRes) {
		return nil, InterpretErrorGeneric(err, workersRes)
	}

	workers := make([]*goclientnew.ExtendedBridgeWorker, 0, len(*workersRes.JSON200))
	for _, worker := range *workersRes.JSON200 {
		workers = append(workers, &worker)
	}
	return workers, nil
}

func apiListAllBridgeWorkers(whereFilter string, selectParam string) ([]*goclientnew.ExtendedBridgeWorker, error) {
	newParams := &goclientnew.ListAllBridgeWorkersParams{}
	if whereFilter != "" {
		newParams.Where = &whereFilter
	}
	if contains != "" {
		newParams.Contains = &contains
	}
	include := "SpaceID"
	newParams.Include = &include
	// Handle select parameter
	selectValue := handleSelectParameter(selectParam, selectFields, func() string {
		baseFields := []string{"Slug", "BridgeWorkerID", "SpaceID", "OrganizationID", "Secret"}
		return buildSelectList("BridgeWorker", "", include, defaultWorkerColumns, workerAliases, workerCustomColumnDependencies, baseFields)
	})
	if selectValue != "" && selectValue != "*" {
		newParams.Select = &selectValue
	}
	workersRes, err := cubClientNew.ListAllBridgeWorkersWithResponse(ctx, newParams)
	if IsAPIError(err, workersRes) {
		return nil, InterpretErrorGeneric(err, workersRes)
	}

	workers := make([]*goclientnew.ExtendedBridgeWorker, 0, len(*workersRes.JSON200))
	for _, worker := range *workersRes.JSON200 {
		workers = append(workers, &worker)
	}
	return workers, nil
}

func getExtendedWorkerSlug(worker *goclientnew.ExtendedBridgeWorker) string {
	return worker.BridgeWorker.Slug
}

func displayExtendedWorkerList(workers []*goclientnew.ExtendedBridgeWorker) {
	table := tableView()
	if !noheader {
		table.SetHeader([]string{"Name", "Condition", "Space", "Last-Seen"})
	}
	for _, worker := range workers {
		spaceSlug := ""
		if worker.Space != nil {
			spaceSlug = worker.Space.Slug
		} else if selectedSpaceID != "*" {
			spaceSlug = selectedSpaceSlug
		}

		lastSeen := worker.BridgeWorker.CreatedAt.Format("2006-01-02 15:04:05")
		if !worker.BridgeWorker.LastSeenAt.IsZero() {
			lastSeen = worker.BridgeWorker.LastSeenAt.Format("2006-01-02 15:04:05")
		}

		table.Append([]string{
			worker.BridgeWorker.Slug,
			worker.BridgeWorker.Condition,
			spaceSlug,
			lastSeen,
		})
	}
	table.Render()
}
