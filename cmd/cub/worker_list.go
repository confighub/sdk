// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var workerListCmd = &cobra.Command{
	Use:         "list",
	Short:       "List workers",
	Long:        getCommandHelp(`List workers in a space or across all spaces. Use --space "*" to list workers across all spaces.`, ""),
	Annotations: map[string]string{"OrgLevel": ""},
	RunE:        workerListCmdRun,
}

// Default columns to display when no custom columns are specified
var defaultWorkerColumns = []string{"BridgeWorker.Slug", "BridgeWorker.Condition", "Space.Slug", "BridgeWorker.LastSeenAt"}

// workerListInclude is the Include parameter for worker list queries.
const workerListInclude = "SpaceID"

// workerBaseSelectFields are the fields always returned by worker list queries.
var workerBaseSelectFields = []string{"Slug", "BridgeWorkerID", "SpaceID", "OrganizationID", "Secret"}

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
	filterID, err := parseFilterFlag(filter)
	if err != nil {
		return err
	}

	workers, err := apiListBridgeworkers(selectedSpaceID, where, selectFields, filterID)
	if err != nil {
		return err
	}
	displayListResults(workers, getExtendedWorkerSlug, displayExtendedWorkerList)
	return nil
}

// apiListBridgeworkers lists bridge workers via the org-level endpoint, scoped to
// a single space by a SpaceID clause unless spaceID is "*" (list across all spaces).
func apiListBridgeworkers(spaceID string, whereFilter string, selectParam string, filterParam string) ([]*goclientnew.ExtendedBridgeWorker, error) {
	where := cubapi.NewWhere(whereFilter)
	if spaceID != "*" {
		where = where.SpaceID(goclientnew.UUID(uuid.MustParse(spaceID)))
	}
	return apiListAllBridgeWorkers(where, selectParam, filterParam)
}

func apiListAllBridgeWorkers(where cubapi.Where, selectParam string, filterParam string) ([]*goclientnew.ExtendedBridgeWorker, error) {
	selectValue := handleSelectParameter(selectParam, selectFields, func() string {
		return buildSelectList("BridgeWorker", nil, workerListInclude, defaultWorkerColumns, workerAliases, workerCustomColumnDependencies, workerBaseSelectFields)
	})
	return cubapi.ListBridgeWorkers(ctx, cubClient, where, cubapi.ListOpts{
		Select:   cubapi.SelectFields(selectValue),
		Include:  workerListInclude,
		Filter:   filterParam,
		Contains: contains,
	})
}

func getExtendedWorkerSlug(worker *goclientnew.ExtendedBridgeWorker) string {
	space := ""
	if worker.Space != nil {
		space = worker.Space.Slug
	}
	return prefixedSlug(space, worker.BridgeWorker.Slug)
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
