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

var viewDeleteCmd = &cobra.Command{
	Use:   "delete [<slug or id>]",
	Short: "Delete a view or multiple views",
	Long: `Delete a view or multiple views using bulk operations.

Single view delete:
  cub view delete my-view

Bulk delete with --where:
Delete multiple views at once based on search criteria.

Examples:
  # Delete all views created before a specific date
  cub view delete --where "CreatedAt < '2024-01-01'"

  # Delete views with specific filters
  cub view delete --where "FilterID = 'filter-uuid'"

  # Delete views with no columns defined
  cub view delete --where "Columns IS NULL OR Columns = '[]'"

  # Delete views across all spaces (requires --space "*")
  cub view delete --space "*" --where "Labels.cleanup = 'true'"

  # Delete specific views by slug
  cub view delete --view old-view,deprecated-view`,
	Args:        cobra.MaximumNArgs(1), // Allow 0 or 1 args (0 for bulk mode)
	RunE:        viewDeleteCmdRun,
	Annotations: map[string]string{"OrgLevel": ""},
}

var (
	viewDeleteIdentifiers []string
)

func init() {
	addStandardDeleteFlags(viewDeleteCmd)
	enableWhereFlag(viewDeleteCmd)
	enableFilterFlag(viewDeleteCmd)
	viewDeleteCmd.Flags().StringSliceVar(&viewDeleteIdentifiers, "view", []string{}, "target specific views by slug or UUID for bulk delete (can be repeated or comma-separated)")
	viewCmd.AddCommand(viewDeleteCmd)
}

func checkViewDeleteConflictingArgs(args []string) bool {
	// Check for bulk delete mode (no positional args)
	isBulkDeleteMode := len(args) == 0

	if isBulkDeleteMode {
		// Check for mutual exclusivity between --view and --where flags
		if len(viewDeleteIdentifiers) > 0 && where != "" {
			failOnError(fmt.Errorf("--view and --where flags are mutually exclusive"))
		}

	} else {
		// Single delete mode validation
		if len(args) != 1 {
			failOnError(fmt.Errorf("single view delete requires exactly one argument: <slug or id>"))
		}

		if filter != "" || where != "" || len(viewDeleteIdentifiers) > 0 {
			failOnError(fmt.Errorf("--filter, --where, or --view can only be specified with no positional arguments"))
		}
	}

	if err := validateSpaceFlag(isBulkDeleteMode); err != nil {
		failOnError(err)
	}

	return isBulkDeleteMode
}

func runBulkViewDelete() error {
	// Parse filter parameter
	filterID, err := parseFilterFlag(filter)
	if err != nil {
		return err
	}

	// Build WHERE clause from view identifiers or use provided where clause
	var effectiveWhere string
	if len(viewDeleteIdentifiers) > 0 {
		whereClause, err := buildWhereClauseFromViews(viewDeleteIdentifiers)
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
	include := "SpaceID,FilterID"
	params := &goclientnew.BulkDeleteViewsParams{
		Where:   &effectiveWhere,
		Include: &include,
	}
	if filterID != "" {
		params.Filter = &filterID
	}

	// Call the bulk delete API
	bulkRes, err := cubClientNew.BulkDeleteViewsWithResponse(ctx, params)
	if cubapi.IsAPIError(err, bulkRes) {
		return cubapi.InterpretErrorGeneric(err, bulkRes)
	}

	// Handle the response
	return handleBulkViewDeleteResponse(bulkRes.JSON200, bulkRes.JSON207, bulkRes.StatusCode(), "delete", effectiveWhere)
}

func viewDeleteCmdRun(cmd *cobra.Command, args []string) error {
	isBulkDeleteMode := checkViewDeleteConflictingArgs(args)

	if isBulkDeleteMode {
		return runBulkViewDelete()
	}

	// Single view delete logic
	viewDetails, err := apiGetViewFromSlug(args[0], "*") // get all fields for now
	if err != nil {
		return err
	}
	deleteRes, err := cubClientNew.DeleteViewWithResponse(ctx, uuid.MustParse(selectedSpaceID), viewDetails.ViewID)
	if cubapi.IsAPIError(err, deleteRes) {
		return cubapi.InterpretErrorGeneric(err, deleteRes)
	}

	displayDeleteResults("view", args[0], viewDetails.ViewID.String(), deleteRes)
	return nil
}

func handleBulkViewDeleteResponse(responses200 *[]goclientnew.DeleteResponse, responses207 *[]goclientnew.DeleteResponse, statusCode int, operationName, contextInfo string) error {
	return displayBulkDeleteResults(responses200, responses207, statusCode, "view", operationName, contextInfo)
}
