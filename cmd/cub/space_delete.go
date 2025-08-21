// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"

	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/spf13/cobra"
)

var spaceDeleteCmd = &cobra.Command{
	Use:   "delete [<slug or id>]",
	Short: "Delete a space or multiple spaces",
	Long: `Delete a space or multiple spaces using bulk operations.

Single space delete:
  cub space delete my-space

Bulk delete with --where:
Delete multiple spaces at once based on search criteria.

Examples:
  # Delete all spaces with specific label
  cub space delete --where "Labels.Environment = 'staging'"

  # Delete spaces created before a date
  cub space delete --where "CreatedAt < '2024-01-01'"

  # Delete specific spaces by slug
  cub space delete --space my-space,another-space`,
	Args: cobra.MaximumNArgs(1), // Allow 0 or 1 args (0 for bulk mode)
	RunE: spaceDeleteCmdRun,
}

var (
	spaceDeleteIdentifiers []string
)

func init() {
	addStandardDeleteFlags(spaceDeleteCmd)
	enableWhereFlag(spaceDeleteCmd)
	spaceDeleteCmd.Flags().StringSliceVar(&spaceDeleteIdentifiers, "space", []string{}, "target specific spaces by slug or UUID for bulk delete (can be repeated or comma-separated)")
	spaceCmd.AddCommand(spaceDeleteCmd)
}

func checkSpaceDeleteConflictingArgs(args []string) bool {
	// Check for bulk delete mode (no positional args with --where or --space)
	isBulkDeleteMode := len(args) == 0 && (where != "" || len(spaceDeleteIdentifiers) > 0)

	if !isBulkDeleteMode && (where != "" || len(spaceDeleteIdentifiers) > 0) {
		failOnError(fmt.Errorf("--where or --space can only be specified with no positional arguments"))
	}

	// Single delete mode validation
	if !isBulkDeleteMode && len(args) != 1 {
		failOnError(fmt.Errorf("single space delete requires exactly one argument: <slug or id>"))
	}

	// Check for mutual exclusivity between --space and --where flags
	if len(spaceDeleteIdentifiers) > 0 && where != "" {
		failOnError(fmt.Errorf("--space and --where flags are mutually exclusive"))
	}

	if isBulkDeleteMode && (where == "" && len(spaceDeleteIdentifiers) == 0) {
		failOnError(fmt.Errorf("bulk delete mode requires --where or --space flags"))
	}

	return isBulkDeleteMode
}

func runBulkSpaceDelete() error {
	// Build WHERE clause from space identifiers or use provided where clause
	var effectiveWhere string
	if len(spaceDeleteIdentifiers) > 0 {
		whereClause, err := buildWhereClauseFromSpaces(spaceDeleteIdentifiers)
		if err != nil {
			return err
		}
		effectiveWhere = whereClause
	} else {
		effectiveWhere = where
	}

	// Note: Space delete is already OrgLevel, so no space constraint needed
	// Build bulk delete parameters
	include := "OrganizationID"
	params := &goclientnew.BulkDeleteSpacesParams{
		Where:   &effectiveWhere,
		Include: &include,
	}

	// Call the bulk delete API
	bulkRes, err := cubClientNew.BulkDeleteSpacesWithResponse(ctx, params)
	if IsAPIError(err, bulkRes) {
		return InterpretErrorGeneric(err, bulkRes)
	}

	// Handle the response
	return handleBulkSpaceDeleteResponse(bulkRes.JSON200, bulkRes.JSON207, bulkRes.StatusCode(), "delete", effectiveWhere)
}

func spaceDeleteCmdRun(cmd *cobra.Command, args []string) error {
	isBulkDeleteMode := checkSpaceDeleteConflictingArgs(args)

	if isBulkDeleteMode {
		return runBulkSpaceDelete()
	}

	// Single space delete logic
	spaceDetails, err := apiGetSpaceFromSlug(args[0], "*") // get all fields for now
	if err != nil {
		return err
	}
	spaceID := spaceDetails.SpaceID
	deleteRes, err := cubClientNew.DeleteSpaceWithResponse(ctx, spaceID)
	if IsAPIError(err, deleteRes) {
		return InterpretErrorGeneric(err, deleteRes)
	}
	displayDeleteResults("space", args[0], spaceID.String())
	return nil
}

func handleBulkSpaceDeleteResponse(responses200 *[]goclientnew.DeleteResponse, responses207 *[]goclientnew.DeleteResponse, statusCode int, operationName, contextInfo string) error {
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
				fmt.Printf("Successfully %sd space: %s\n", operationName, resp.Message)
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
		fmt.Printf("  Success: %d space(s)\n", successCount)
		if failureCount > 0 {
			fmt.Printf("  Failed: %d space(s)\n", failureCount)
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
