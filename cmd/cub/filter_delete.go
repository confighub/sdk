// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"

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
  cub filter delete --filter my-filter,another-filter`,
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
	filterDeleteCmd.Flags().StringSliceVar(&filterDeleteIdentifiers, "filter", []string{}, "target specific filters by slug or UUID for bulk delete (can be repeated or comma-separated)")
	filterCmd.AddCommand(filterDeleteCmd)
}

func checkFilterDeleteConflictingArgs(args []string) bool {
	// Check for bulk delete mode (no positional args with --where or --filter)
	isBulkDeleteMode := len(args) == 0 && (where != "" || len(filterDeleteIdentifiers) > 0)

	if !isBulkDeleteMode && (where != "" || len(filterDeleteIdentifiers) > 0) {
		failOnError(fmt.Errorf("--where or --filter can only be specified with no positional arguments"))
	}

	// Single delete mode validation
	if !isBulkDeleteMode && len(args) != 1 {
		failOnError(fmt.Errorf("single filter delete requires exactly one argument: <slug or id>"))
	}

	// Check for mutual exclusivity between --filter and --where flags
	if len(filterDeleteIdentifiers) > 0 && where != "" {
		failOnError(fmt.Errorf("--filter and --where flags are mutually exclusive"))
	}

	if isBulkDeleteMode && (where == "" && len(filterDeleteIdentifiers) == 0) {
		failOnError(fmt.Errorf("bulk delete mode requires --where or --filter flags"))
	}

	if err := validateSpaceFlag(isBulkDeleteMode); err != nil {
		failOnError(err)
	}

	return isBulkDeleteMode
}

func runBulkFilterDelete() error {
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

	// Call the bulk delete API
	bulkRes, err := cubClientNew.BulkDeleteFiltersWithResponse(ctx, params)
	if IsAPIError(err, bulkRes) {
		return InterpretErrorGeneric(err, bulkRes)
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
	filterDetails, err := apiGetFilterFromSlug(args[0], "*") // get all fields for now
	if err != nil {
		return err
	}
	deleteRes, err := cubClientNew.DeleteFilterWithResponse(ctx, uuid.MustParse(selectedSpaceID), filterDetails.FilterID)
	if IsAPIError(err, deleteRes) {
		return InterpretErrorGeneric(err, deleteRes)
	}

	displayDeleteResults("filter", args[0], filterDetails.FilterID.String())
	return nil
}

func handleBulkFilterDeleteResponse(responses200 *[]goclientnew.DeleteResponse, responses207 *[]goclientnew.DeleteResponse, statusCode int, operationName, contextInfo string) error {
	var responses *[]goclientnew.DeleteResponse
	if statusCode == 200 && responses200 != nil {
		responses = responses200
	} else if statusCode == 207 && responses207 != nil {
		responses = responses207
	} else {
		return fmt.Errorf("unexpected status code %d or no response data", statusCode)
	}

	if responses == nil {
		return fmt.Errorf("no response data received")
	}

	successCount := 0
	failureCount := 0
	var failures []string

	for _, resp := range *responses {
		if resp.Error == nil {
			successCount++
			if verbose {
				fmt.Printf("Successfully %sd filter: %s\n", operationName, resp.Message)
			}
		} else {
			failureCount++
			errorMsg := "unknown error"
			if resp.Error != nil && resp.Error.Message != "" {
				errorMsg = resp.Error.Message
			}
			failures = append(failures, fmt.Sprintf("  - %s", errorMsg))
		}
	}

	// Display summary
	if !jsonOutput {
		fmt.Printf("\nBulk %s operation completed:\n", operationName)
		fmt.Printf("  Success: %d filter(s)\n", successCount)
		if failureCount > 0 {
			fmt.Printf("  Failed: %d filter(s)\n", failureCount)
			if verbose && len(failures) > 0 {
				fmt.Println("\nFailures:")
				for _, failure := range failures {
					fmt.Println(failure)
				}
			}
		}
		if contextInfo != "" {
			fmt.Printf("  Context: %s\n", contextInfo)
		}
	}

	// Return success only if all operations succeeded
	if statusCode == 207 || failureCount > 0 {
		return fmt.Errorf("bulk %s partially failed: %d succeeded, %d failed", operationName, successCount, failureCount)
	}

	return nil
}
