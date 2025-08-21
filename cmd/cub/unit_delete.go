// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"

	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var unitDeleteCmd = &cobra.Command{
	Use:   "delete [<slug or id>]",
	Short: "Delete a unit or multiple units",
	Long: `Delete a unit or multiple units using bulk operations.

Single unit delete:
  cub unit delete my-unit

Bulk delete with --where:
Delete multiple units at once based on search criteria.

Examples:
  # Delete all units with specific label
  cub unit delete --where "Labels.Tier = 'backend'"

  # Delete units across all spaces (requires --space "*")
  cub unit delete --space "*" --where "Labels.cleanup = 'true'"

  # Delete specific units by slug
  cub unit delete --unit my-unit,another-unit`,
	Args:        cobra.MaximumNArgs(1), // Allow 0 or 1 args (0 for bulk mode)
	RunE:        unitDeleteCmdRun,
	Annotations: map[string]string{"OrgLevel": ""},
}

func init() {
	addStandardDeleteFlags(unitDeleteCmd)
	enableWhereFlag(unitDeleteCmd)
	unitDeleteCmd.Flags().StringSliceVar(&unitIdentifiers, "unit", []string{}, "target specific units by slug or UUID for bulk delete (can be repeated or comma-separated)")
	unitCmd.AddCommand(unitDeleteCmd)
}

func checkUnitDeleteConflictingArgs(args []string) bool {
	// Check for bulk delete mode (no positional args with --where or --unit)
	isBulkDeleteMode := len(args) == 0 && (where != "" || len(unitIdentifiers) > 0)

	if !isBulkDeleteMode && (where != "" || len(unitIdentifiers) > 0) {
		failOnError(fmt.Errorf("--where or --unit can only be specified with no positional arguments"))
	}

	// Single delete mode validation
	if !isBulkDeleteMode && len(args) != 1 {
		failOnError(fmt.Errorf("single unit delete requires exactly one argument: <slug or id>"))
	}

	// Check for mutual exclusivity between --unit and --where flags
	if len(unitIdentifiers) > 0 && where != "" {
		failOnError(fmt.Errorf("--unit and --where flags are mutually exclusive"))
	}

	if isBulkDeleteMode && (where == "" && len(unitIdentifiers) == 0) {
		failOnError(fmt.Errorf("bulk delete mode requires --where or --unit flags"))
	}

	if err := validateSpaceFlag(isBulkDeleteMode); err != nil {
		failOnError(err)
	}

	return isBulkDeleteMode
}

func runBulkUnitDelete() error {
	// Build WHERE clause from unit identifiers or use provided where clause
	var effectiveWhere string
	if len(unitIdentifiers) > 0 {
		whereClause, err := buildWhereClauseFromUnits(unitIdentifiers)
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
	include := "UnitEventID,TargetID,UpstreamUnitID,SpaceID"
	params := &goclientnew.BulkDeleteUnitsParams{
		Where:   &effectiveWhere,
		Include: &include,
	}

	// Call the bulk delete API
	bulkRes, err := cubClientNew.BulkDeleteUnitsWithResponse(ctx, params)
	if IsAPIError(err, bulkRes) {
		return InterpretErrorGeneric(err, bulkRes)
	}

	// Handle the response
	return handleBulkUnitDeleteResponse(bulkRes.JSON200, bulkRes.JSON207, bulkRes.StatusCode(), "delete", effectiveWhere)
}

func unitDeleteCmdRun(cmd *cobra.Command, args []string) error {
	isBulkDeleteMode := checkUnitDeleteConflictingArgs(args)

	if isBulkDeleteMode {
		return runBulkUnitDelete()
	}

	// Single unit delete logic
	unitDetails, err := apiGetUnitFromSlug(args[0], "*") // get all fields for now
	if err != nil {
		return err
	}
	deleteRes, err := cubClientNew.DeleteUnitWithResponse(ctx, uuid.MustParse(selectedSpaceID), unitDetails.UnitID)
	if IsAPIError(err, deleteRes) {
		return InterpretErrorGeneric(err, deleteRes)
	}

	displayDeleteResults("unit", args[0], unitDetails.UnitID.String())
	return nil
}

func handleBulkUnitDeleteResponse(responses200 *[]goclientnew.DeleteResponse, responses207 *[]goclientnew.DeleteResponse, statusCode int, operationName, contextInfo string) error {
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
				fmt.Printf("Successfully %sd unit: %s\n", operationName, resp.Message)
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
		fmt.Printf("  Success: %d unit(s)\n", successCount)
		if failureCount > 0 {
			fmt.Printf("  Failed: %d unit(s)\n", failureCount)
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
