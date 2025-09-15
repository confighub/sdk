// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"

	"github.com/confighub/sdk/cubapi"
	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var bridgeworkerDeleteCmd = &cobra.Command{
	Use:   "delete [<name>]",
	Short: "Delete a bridgeworker or multiple bridgeworkers",
	Long: `Delete a bridgeworker or multiple bridgeworkers using bulk operations.

Single bridgeworker delete:
  cub worker delete my-worker

Bulk delete with --where:
Delete multiple bridgeworkers at once based on search criteria.

Examples:
  # Delete all disabled bridgeworkers
  cub worker delete --where "Disabled = true"

  # Delete bridgeworkers with specific status
  cub worker delete --where "Status = 'inactive'"

  # Delete bridgeworkers across all spaces (requires --space "*")
  cub worker delete --space "*" --where "Labels.cleanup = 'true'"

  # Delete specific bridgeworkers by name
  cub worker delete --worker my-worker,another-worker`,
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
	bridgeworkerDeleteCmd.Flags().StringSliceVar(&workerDeleteIdentifiers, "worker", []string{}, "target specific bridgeworkers by name or UUID for bulk delete (can be repeated or comma-separated)")
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
			failOnError(fmt.Errorf("single bridgeworker delete requires exactly one argument: <name>"))
		}

		if filter != "" || where != "" || len(workerDeleteIdentifiers) > 0 {
			failOnError(fmt.Errorf("--where or --worker can only be specified with no positional arguments"))
		}
	}

	if err := validateSpaceFlag(isBulkDeleteMode); err != nil {
		failOnError(err)
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
	if filterID != "" {
		params.Filter = &filterID
	}

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
	worker, err := apiGetBridgeWorkerFromSlug(args[0], "*") // get all fields for now
	if err != nil {
		return err
	}
	deleteRes, err := cubClientNew.DeleteBridgeWorkerWithResponse(ctx, uuid.MustParse(selectedSpaceID), worker.BridgeWorkerID)
	if cubapi.IsAPIError(err, deleteRes) {
		return cubapi.InterpretErrorGeneric(err, deleteRes)
	}

	displayDeleteResults("bridge_worker", args[0], worker.BridgeWorkerID.String(), deleteRes)
	return nil
}

func handleBulkWorkerDeleteResponse(responses200 *[]goclientnew.DeleteResponse, responses207 *[]goclientnew.DeleteResponse, statusCode int, operationName, contextInfo string) error {
	return displayBulkDeleteResults(responses200, responses207, statusCode, "bridgeworker", operationName, contextInfo)
}
