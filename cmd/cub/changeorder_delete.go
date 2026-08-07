// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var changeorderDeleteCmd = &cobra.Command{
	Use:   "delete [<slug or id>]",
	Short: "Delete a changeorder or multiple changeorders",
	Long: getCommandHelp(`Delete a changeorder or multiple changeorders using bulk operations.

Single changeorder delete:
`+"```"+`
  cub changeorder delete my-changeorder
`+"```"+`

Bulk delete with --where:

Delete multiple changeorders at once based on search criteria.

Examples:
`+"```"+`
  # Delete all changeorders created before a specific date
  cub changeorder delete --where "CreatedAt < '2024-01-01'"

  # Delete changeorders with specific descriptions
  cub changeorder delete --where "Description LIKE '%deprecated%'"

  # Delete changeorders across all spaces (requires --space "*")
  cub changeorder delete --space "*" --where "Labels.cleanup = 'true'"

  # Delete specific changeorders by slug
  cub changeorder delete --changeorder old-changeorder,deprecated-changeorder
`+"```"+`
`, ""),
	Args:        cobra.MaximumNArgs(1), // Allow 0 or 1 args (0 for bulk mode)
	RunE:        changeorderDeleteCmdRun,
	Annotations: map[string]string{"OrgLevel": ""},
}

var (
	changeorderDeleteIdentifiers []string
)

func init() {
	addStandardDeleteFlags(changeorderDeleteCmd)
	enableWhereFlag(changeorderDeleteCmd)
	enableFilterFlag(changeorderDeleteCmd)
	changeorderDeleteCmd.Flags().StringSliceVar(&changeorderDeleteIdentifiers, "changeorder", []string{}, "target specific changeorders by slug or UUID for bulk delete (can be repeated or comma-separated)")
	changeorderCmd.AddCommand(changeorderDeleteCmd)
}

func checkChangeOrderDeleteConflictingArgs(args []string) bool {
	// Check for bulk delete mode: no positional args
	isBulkDeleteMode := len(args) == 0

	if isBulkDeleteMode {
		// Check for mutual exclusivity between --changeorder and --where flags
		if len(changeorderDeleteIdentifiers) > 0 && where != "" {
			failOnError(fmt.Errorf("--changeorder and --where flags are mutually exclusive"))
		}

	} else {
		// Single delete mode validation
		if len(args) != 1 {
			failOnError(fmt.Errorf("single changeorder delete requires exactly one argument: <slug or id>"))
		}

		if filter != "" || where != "" || len(changeorderDeleteIdentifiers) > 0 {
			failOnError(fmt.Errorf("--filter, --where, or --changeorder can only be specified with no positional arguments"))
		}
	}

	if err := validateSpaceFlag(isBulkDeleteMode); err != nil {
		failOnError(err)
	}

	return isBulkDeleteMode
}

func runBulkChangeOrderDelete() error {
	// Parse filter parameter
	filterID, err := parseFilterFlag(filter)
	if err != nil {
		return err
	}

	// Build WHERE clause from changeorder identifiers or use provided where clause
	var effectiveWhere string
	if len(changeorderDeleteIdentifiers) > 0 {
		whereClause, err := buildWhereClauseFromChangeOrders(changeorderDeleteIdentifiers)
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
	include := "SpaceID,FilterID,StartTagID,EndTagID"
	params := &goclientnew.BulkDeleteChangeOrdersParams{
		Where:   &effectiveWhere,
		Include: &include,
	}
	if filterID != "" {
		params.Filter = &filterID
	}

	// Call the bulk delete API
	bulkRes, err := cubClientNew.BulkDeleteChangeOrdersWithResponse(ctx, params)
	if cubapi.IsAPIError(err, bulkRes) {
		return cubapi.InterpretErrorGeneric(err, bulkRes)
	}

	// Handle the response
	return handleBulkChangeOrderDeleteResponse(bulkRes.JSON200, bulkRes.JSON207, bulkRes.StatusCode(), "delete", effectiveWhere)
}

func changeorderDeleteCmdRun(cmd *cobra.Command, args []string) error {
	isBulkDeleteMode := checkChangeOrderDeleteConflictingArgs(args)

	if isBulkDeleteMode {
		return runBulkChangeOrderDelete()
	}

	// Single changeorder delete logic
	changeorderDetails, err := apiGetChangeOrderFromSlug(args[0], "*") // get all fields for now
	if err != nil {
		return err
	}
	deleteRes, err := cubClientNew.DeleteChangeOrderWithResponse(ctx, uuid.MustParse(selectedSpaceID), changeorderDetails.ChangeOrderID)
	if cubapi.IsAPIError(err, deleteRes) {
		return cubapi.InterpretErrorGeneric(err, deleteRes)
	}

	displayDeleteResults("changeorder", args[0], changeorderDetails.ChangeOrderID.String(), deleteRes)
	return nil
}

func handleBulkChangeOrderDeleteResponse(responses200 *[]goclientnew.DeleteResponse, responses207 *[]goclientnew.DeleteResponse, statusCode int, operationName, contextInfo string) error {
	return displayBulkDeleteResults(responses200, responses207, statusCode, "changeorder", operationName, contextInfo)
}
