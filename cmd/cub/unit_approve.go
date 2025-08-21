// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"

	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var unitApproveCmd = &cobra.Command{
	Use:   "approve [<unit-slug>]",
	Short: "Approve a unit or multiple units",
	Long: `Approve a unit or multiple units using bulk operations.

Single unit approve:
  cub unit approve my-unit

Bulk approve with --where:
Approve multiple units at once based on search criteria.

Examples:
  # Approve all units with specific label
  cub unit approve --where "Labels.Tier = 'backend'"

  # Approve units across all spaces (requires --space "*")
  cub unit approve --space "*" --where "Slug = 'backend'"

  # Approve specific units by slug
  cub unit approve --unit my-unit,another-unit`,
	Args:        cobra.MaximumNArgs(1), // Allow 0 or 1 args (0 for bulk mode)
	RunE:        unitApproveCmdRun,
	Annotations: map[string]string{"OrgLevel": ""},
}

func init() {
	enableWhereFlag(unitApproveCmd)
	unitApproveCmd.Flags().StringSliceVar(&unitIdentifiers, "unit", []string{}, "target specific units by slug or UUID for bulk approve (can be repeated or comma-separated)")
	unitCmd.AddCommand(unitApproveCmd)
}

func checkUnitApproveConflictingArgs(args []string) bool {
	// Check for bulk approve mode (no positional args with --where or --unit)
	isBulkApproveMode := len(args) == 0 && (where != "" || len(unitIdentifiers) > 0)

	if !isBulkApproveMode && (where != "" || len(unitIdentifiers) > 0) {
		failOnError(fmt.Errorf("--where or --unit can only be specified with no positional arguments"))
	}

	// Single approve mode validation
	if !isBulkApproveMode && len(args) != 1 {
		failOnError(fmt.Errorf("single unit approve requires exactly one argument: <slug or id>"))
	}

	// Check for mutual exclusivity between --unit and --where flags
	if len(unitIdentifiers) > 0 && where != "" {
		failOnError(fmt.Errorf("--unit and --where flags are mutually exclusive"))
	}

	if isBulkApproveMode && (where == "" && len(unitIdentifiers) == 0) {
		failOnError(fmt.Errorf("bulk approve mode requires --where or --unit flags"))
	}

	return isBulkApproveMode
}

func runBulkUnitApprove() error {
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

	// Build bulk approve parameters
	include := "UnitEventID,TargetID,UpstreamUnitID,SpaceID"
	params := &goclientnew.BulkApproveUnitsParams{
		Where:   &effectiveWhere,
		Include: &include,
	}

	// Call the bulk approve API
	bulkRes, err := cubClientNew.BulkApproveUnitsWithResponse(ctx, params)
	if err != nil {
		return err
	}

	// Handle the response
	return handleBulkUnitApproveResponse(bulkRes.JSON200, bulkRes.JSON207, bulkRes.StatusCode(), "approve", effectiveWhere)
}

func unitApproveCmdRun(cmd *cobra.Command, args []string) error {
	isBulkApproveMode := checkUnitApproveConflictingArgs(args)

	if isBulkApproveMode {
		return runBulkUnitApprove()
	}

	// Single unit approve logic
	configUnit, err := apiGetUnitFromSlug(args[0], "*") // get all fields for now
	if err != nil {
		return err
	}

	approveRes, err := cubClientNew.ApproveUnitWithResponse(ctx, uuid.MustParse(selectedSpaceID), configUnit.UnitID)
	if IsAPIError(err, approveRes) {
		return InterpretErrorGeneric(err, approveRes)
	}

	fmt.Printf("Unit %s (%s) has been approved\n", args[0], configUnit.UnitID.String())
	return nil
}

func handleBulkUnitApproveResponse(responses200 *[]goclientnew.ApproveResponse, responses207 *[]goclientnew.ApproveResponse, statusCode int, operationName, contextInfo string) error {
	var responses *[]goclientnew.ApproveResponse
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
