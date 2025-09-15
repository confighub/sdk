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

var filterDeleteCmd = &cobra.Command{
	Use:   "delete [<slug or id>]",
	Short: "Delete a filter or multiple filters",
	Long: `Delete a filter or multiple filters using bulk operations.

Single filter delete:
  cub filter delete my-filter

Bulk delete with --where:
Delete multiple filters at once based on search criteria.

Examples:
  # Delete all filters for Units
  cub filter delete --where "From = 'Unit'"

  # Delete filters with specific resource type
  cub filter delete --where "ResourceType = 'apps/v1/Deployment'"

  # Delete filters across all spaces (requires --space "*")
  cub filter delete --space "*" --where "Labels.cleanup = 'true'"

  # Delete specific filters by slug
  cub filter delete --filter-entity my-filter,another-filter`,
	Args:        cobra.MaximumNArgs(1), // Allow 0 or 1 args (0 for bulk mode)
	RunE:        filterDeleteCmdRun,
	Annotations: map[string]string{"OrgLevel": ""},
}

var (
	filterDeleteIdentifiers []string
)

func init() {
	addStandardDeleteFlags(filterDeleteCmd)
	enableWhereFlag(filterDeleteCmd)
	enableFilterFlag(filterDeleteCmd)
	filterDeleteCmd.Flags().StringSliceVar(&filterDeleteIdentifiers, "filter-entity", []string{}, "target specific filters by slug or UUID for bulk delete (can be repeated or comma-separated)")
	filterCmd.AddCommand(filterDeleteCmd)
}

func checkFilterDeleteConflictingArgs(args []string) bool {
	// Check for bulk delete mode: no positional args
	isBulkDeleteMode := len(args) == 0

	if isBulkDeleteMode {
		// Check for mutual exclusivity between --filter-entity and --where flags
		if len(filterDeleteIdentifiers) > 0 && where != "" {
			failOnError(fmt.Errorf("--filter-entity and --where flags are mutually exclusive"))
		}

	} else {
		// Single delete mode validation
		if len(args) != 1 {
			failOnError(fmt.Errorf("single filter delete requires exactly one argument: <slug or id>"))
		}

		if filter != "" || where != "" || len(filterDeleteIdentifiers) > 0 {
			failOnError(fmt.Errorf("--filter, --where, or --filter-entity can only be specified with no positional arguments"))
		}
	}

	if err := validateSpaceFlag(isBulkDeleteMode); err != nil {
		failOnError(err)
	}

	return isBulkDeleteMode
}

func runBulkFilterDelete() error {
	// Parse filter parameter
	filterID, err := parseFilterFlag(filter)
	if err != nil {
		return err
	}

	// Build WHERE clause from filter identifiers or use provided where clause
	var effectiveWhere string
	if len(filterDeleteIdentifiers) > 0 {
		whereClause, err := buildWhereClauseFromFilters(filterDeleteIdentifiers)
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
	include := "SpaceID,FromSpaceID"
	params := &goclientnew.BulkDeleteFiltersParams{
		Where:   &effectiveWhere,
		Include: &include,
	}
	if filterID != "" {
		params.Filter = &filterID
	}

	// NOTE: It may be possible to use a filter that we're deleting, but to warn about that
	// we'd have to do a list first

	// Call the bulk delete API
	bulkRes, err := cubClientNew.BulkDeleteFiltersWithResponse(ctx, params)
	if cubapi.IsAPIError(err, bulkRes) {
		return cubapi.InterpretErrorGeneric(err, bulkRes)
	}

	// Handle the response
	return handleBulkFilterDeleteResponse(bulkRes.JSON200, bulkRes.JSON207, bulkRes.StatusCode(), "delete", effectiveWhere)
}

func filterDeleteCmdRun(cmd *cobra.Command, args []string) error {
	isBulkDeleteMode := checkFilterDeleteConflictingArgs(args)

	if isBulkDeleteMode {
		return runBulkFilterDelete()
	}

	// Single filter delete logic
	filterDetails, err := apiGetFilterFromSlug(args[0], "*", selectedSpaceID) // get all fields for now
	if err != nil {
		return err
	}
	deleteRes, err := cubClientNew.DeleteFilterWithResponse(ctx, uuid.MustParse(selectedSpaceID), filterDetails.FilterID)
	if cubapi.IsAPIError(err, deleteRes) {
		return cubapi.InterpretErrorGeneric(err, deleteRes)
	}

	displayDeleteResults("filter", args[0], filterDetails.FilterID.String(), deleteRes)
	return nil
}

func handleBulkFilterDeleteResponse(responses200 *[]goclientnew.DeleteResponse, responses207 *[]goclientnew.DeleteResponse, statusCode int, operationName, contextInfo string) error {
	return displayBulkDeleteResults(responses200, responses207, statusCode, "filter", operationName, contextInfo)
}
