// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"

	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var invocationDeleteCmd = &cobra.Command{
	Use:   "delete [<slug or id>]",
	Short: "Delete an invocation or multiple invocations",
	Long: `Delete an invocation or multiple invocations using bulk operations.

Single invocation delete:
  cub invocation delete my-invocation

Bulk delete with --where:
Delete multiple invocations at once based on search criteria.

Examples:
  # Delete all invocations for a specific function
  cub invocation delete --where "FunctionName = 'validate'"

  # Delete invocations for specific toolchain
  cub invocation delete --where "ToolchainType = 'Kubernetes/YAML'"

  # Delete invocations across all spaces (requires --space "*")
  cub invocation delete --space "*" --where "Labels.cleanup = 'true'"

  # Delete specific invocations by slug
  cub invocation delete --invocation my-invocation,another-invocation`,
	Args:        cobra.MaximumNArgs(1), // Allow 0 or 1 args (0 for bulk mode)
	RunE:        invocationDeleteCmdRun,
	Annotations: map[string]string{"OrgLevel": ""},
}

var (
	invocationDeleteIdentifiers []string
)

func init() {
	addStandardDeleteFlags(invocationDeleteCmd)
	enableWhereFlag(invocationDeleteCmd)
	invocationDeleteCmd.Flags().StringSliceVar(&invocationDeleteIdentifiers, "invocation", []string{}, "target specific invocations by slug or UUID for bulk delete (can be repeated or comma-separated)")
	invocationCmd.AddCommand(invocationDeleteCmd)
}

func checkInvocationDeleteConflictingArgs(args []string) bool {
	// Check for bulk delete mode (no positional args with --where or --invocation)
	isBulkDeleteMode := len(args) == 0 && (where != "" || len(invocationDeleteIdentifiers) > 0)

	if !isBulkDeleteMode && (where != "" || len(invocationDeleteIdentifiers) > 0) {
		failOnError(fmt.Errorf("--where or --invocation can only be specified with no positional arguments"))
	}

	// Single delete mode validation
	if !isBulkDeleteMode && len(args) != 1 {
		failOnError(fmt.Errorf("single invocation delete requires exactly one argument: <slug or id>"))
	}

	// Check for mutual exclusivity between --invocation and --where flags
	if len(invocationDeleteIdentifiers) > 0 && where != "" {
		failOnError(fmt.Errorf("--invocation and --where flags are mutually exclusive"))
	}

	if isBulkDeleteMode && (where == "" && len(invocationDeleteIdentifiers) == 0) {
		failOnError(fmt.Errorf("bulk delete mode requires --where or --invocation flags"))
	}

	if err := validateSpaceFlag(isBulkDeleteMode); err != nil {
		failOnError(err)
	}

	return isBulkDeleteMode
}

func runBulkInvocationDelete() error {
	// Build WHERE clause from invocation identifiers or use provided where clause
	var effectiveWhere string
	if len(invocationDeleteIdentifiers) > 0 {
		whereClause, err := buildWhereClauseFromInvocations(invocationDeleteIdentifiers)
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
	include := "SpaceID,BridgeWorkerID"
	params := &goclientnew.BulkDeleteInvocationsParams{
		Where:   &effectiveWhere,
		Include: &include,
	}

	// Call the bulk delete API
	bulkRes, err := cubClientNew.BulkDeleteInvocationsWithResponse(ctx, params)
	if IsAPIError(err, bulkRes) {
		return InterpretErrorGeneric(err, bulkRes)
	}

	// Handle the response
	return handleBulkInvocationDeleteResponse(bulkRes.JSON200, bulkRes.JSON207, bulkRes.StatusCode(), "delete", effectiveWhere)
}

func invocationDeleteCmdRun(cmd *cobra.Command, args []string) error {
	isBulkDeleteMode := checkInvocationDeleteConflictingArgs(args)

	if isBulkDeleteMode {
		return runBulkInvocationDelete()
	}

	// Single invocation delete logic
	invocationDetails, err := apiGetInvocationFromSlug(args[0], "*") // get all fields for now
	if err != nil {
		return err
	}
	deleteRes, err := cubClientNew.DeleteInvocationWithResponse(ctx, uuid.MustParse(selectedSpaceID), invocationDetails.InvocationID)
	if IsAPIError(err, deleteRes) {
		return InterpretErrorGeneric(err, deleteRes)
	}

	displayDeleteResults("invocation", args[0], invocationDetails.InvocationID.String())
	return nil
}

func handleBulkInvocationDeleteResponse(responses200 *[]goclientnew.DeleteResponse, responses207 *[]goclientnew.DeleteResponse, statusCode int, operationName, contextInfo string) error {
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
				fmt.Printf("Successfully %sd invocation: %s\n", operationName, resp.Message)
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
		fmt.Printf("  Success: %d invocation(s)\n", successCount)
		if failureCount > 0 {
			fmt.Printf("  Failed: %d invocation(s)\n", failureCount)
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
