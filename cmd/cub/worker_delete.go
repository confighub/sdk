// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/spf13/cobra"
)

var bridgeworkerDeleteCmd = &cobra.Command{
	Use:   "delete [<name>]",
	Short: "Delete a worker or multiple workers",
	Long: getCommandHelp(`Delete a worker or multiple workers using bulk operations.

Single worker delete:
`+"```"+`
  cub worker delete --space my-space my-worker
`+"```"+`

Bulk delete with --where:

Delete multiple workers at once based on search criteria.

Examples:
`+"```"+`
  # Delete the workers in a space that are no longer connected
  cub worker delete --space my-space --where "Condition = 'Disconnected'"

  # Delete workers across the organization: without --space, --where spans every space
  cub worker delete --where "Labels.cleanup = 'true'"

  # Delete specific workers by name
  cub worker delete --space my-space --worker my-worker,another-worker
`+"```"+`

A worker is not deleted while Triggers or Invocations name it, and --detach does not clear
them; the error names them. Point them at another worker first (--worker on cub trigger update
or cub invocation update), or delete them.
`, ""),
	Args:        cobra.MaximumNArgs(1), // Allow 0 or 1 args (0 for bulk mode)
	RunE:        bridgeworkerDeleteCmdRun,
	Annotations: map[string]string{"OrgLevel": ""},
}

var (
	workerDeleteIdentifiers []string
)

func init() {
	addStandardDeleteFlags(bridgeworkerDeleteCmd)
	enableWhereFlag(bridgeworkerDeleteCmd)
	enableFilterFlag(bridgeworkerDeleteCmd)
	bridgeworkerDeleteCmd.Flags().StringSliceVar(&workerDeleteIdentifiers, "worker", []string{}, "target specific workers by name or UUID for bulk delete (can be repeated or comma-separated)")
	addDetachFlag(bridgeworkerDeleteCmd)
	workerCmd.AddCommand(bridgeworkerDeleteCmd)
}

func checkWorkerDeleteConflictingArgs(args []string) bool {
	// Check for bulk delete mode (no positional args)
	isBulkDeleteMode := len(args) == 0

	if isBulkDeleteMode {
		// Check for mutual exclusivity between --worker and --where flags
		if len(workerDeleteIdentifiers) > 0 && where != "" {
			failOnError(fmt.Errorf("--worker and --where flags are mutually exclusive"))
		}

	} else {
		// Single delete mode validation
		if len(args) != 1 {
			failOnError(fmt.Errorf("single worker delete requires exactly one argument: <name>"))
		}

		if filter != "" || where != "" || len(workerDeleteIdentifiers) > 0 {
			failOnError(fmt.Errorf("--where or --worker can only be specified with no positional arguments"))
		}
	}

	return isBulkDeleteMode
}

func buildWhereClauseFromWorkers(workerIds []string) (string, error) {
	return buildWhereClauseFromIdentifiers(workerIds, "BridgeWorkerID", "Name")
}

func runBulkWorkerDelete() error {
	// Parse filter parameter
	filterID, err := parseFilterFlag(filter)
	if err != nil {
		return err
	}

	// Build WHERE clause from worker identifiers or use provided where clause
	var effectiveWhere string
	if len(workerDeleteIdentifiers) > 0 {
		whereClause, err := buildWhereClauseFromWorkers(workerDeleteIdentifiers)
		if err != nil {
			return err
		}
		effectiveWhere = whereClause
	} else {
		effectiveWhere = where
	}

	// Add space constraint to the where clause only if not org level
	effectiveWhere = addSpaceIDToWhereClause(effectiveWhere, selectedSpaceID)

	// Build bulk delete parameters
	include := "SpaceID"
	params := &goclientnew.BulkDeleteBridgeWorkersParams{
		Where:   &effectiveWhere,
		Include: &include,
	}
	params.IncludeHidden = includeHiddenParam()
	if filterID != "" {
		params.Filter = &filterID
	}
	params.Detach = detachParam()

	// Call the bulk delete API
	bulkRes, err := cubClientNew.BulkDeleteBridgeWorkersWithResponse(ctx, params)
	if cubapi.IsAPIError(err, bulkRes) {
		return cubapi.InterpretErrorGeneric(err, bulkRes)
	}

	// Handle the response
	return handleBulkWorkerDeleteResponse(bulkRes.JSON200, bulkRes.JSON207, bulkRes.StatusCode(), "delete", effectiveWhere)
}

func bridgeworkerDeleteCmdRun(cmd *cobra.Command, args []string) error {
	isBulkDeleteMode := checkWorkerDeleteConflictingArgs(args)

	if isBulkDeleteMode {
		return runBulkWorkerDelete()
	}

	// Single bridgeworker delete logic
	worker, err := resolveWorker(args[0], selectedSpaceID, "*") // get all fields for now
	if err != nil {
		return err
	}
	deleteRes, err := cubClientNew.DeleteBridgeWorkerWithResponse(ctx, worker.BridgeWorker.SpaceID, worker.BridgeWorker.BridgeWorkerID,
		&goclientnew.DeleteBridgeWorkerParams{Detach: detachParam()})
	if cubapi.IsAPIError(err, deleteRes) {
		return cubapi.InterpretErrorGeneric(err, deleteRes)
	}

	displayDeleteResults("bridge_worker", args[0], worker.BridgeWorker.BridgeWorkerID.String(), deleteRes)
	return nil
}

func handleBulkWorkerDeleteResponse(responses200 *[]goclientnew.DeleteResponse, responses207 *[]goclientnew.DeleteResponse, statusCode int, operationName, contextInfo string) error {
	return displayBulkDeleteResults(responses200, responses207, statusCode, "bridgeworker", operationName, contextInfo)
}
