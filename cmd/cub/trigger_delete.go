// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"

	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var triggerDeleteCmd = &cobra.Command{
	Use:   "delete [<slug or id>]",
	Short: "Delete a trigger or multiple triggers",
	Long: `Delete a trigger or multiple triggers using bulk operations.

Single trigger delete:
  cub trigger delete my-trigger

Bulk delete with --where:
Delete multiple triggers at once based on search criteria.

Examples:
  # Delete all disabled triggers
  cub trigger delete --where "Disabled = true"

  # Delete triggers for specific function
  cub trigger delete --where "FunctionName = 'validate'"

  # Delete triggers across all spaces (requires --space "*")
  cub trigger delete --space "*" --where "Labels.cleanup = 'true'"

  # Delete specific triggers by slug
  cub trigger delete --trigger my-trigger,another-trigger`,
	Args:        cobra.MaximumNArgs(1), // Allow 0 or 1 args (0 for bulk mode)
	RunE:        triggerDeleteCmdRun,
	Annotations: map[string]string{"OrgLevel": ""},
}

var (
	triggerDeleteIdentifiers []string
)

func init() {
	addStandardDeleteFlags(triggerDeleteCmd)
	enableWhereFlag(triggerDeleteCmd)
	triggerDeleteCmd.Flags().StringSliceVar(&triggerDeleteIdentifiers, "trigger", []string{}, "target specific triggers by slug or UUID for bulk delete (can be repeated or comma-separated)")
	triggerCmd.AddCommand(triggerDeleteCmd)
}

func checkTriggerDeleteConflictingArgs(args []string) bool {
	// Check for bulk delete mode (no positional args with --where or --trigger)
	isBulkDeleteMode := len(args) == 0 && (where != "" || len(triggerDeleteIdentifiers) > 0)

	if !isBulkDeleteMode && (where != "" || len(triggerDeleteIdentifiers) > 0) {
		failOnError(fmt.Errorf("--where or --trigger can only be specified with no positional arguments"))
	}

	// Single delete mode validation
	if !isBulkDeleteMode && len(args) != 1 {
		failOnError(fmt.Errorf("single trigger delete requires exactly one argument: <slug or id>"))
	}

	// Check for mutual exclusivity between --trigger and --where flags
	if len(triggerDeleteIdentifiers) > 0 && where != "" {
		failOnError(fmt.Errorf("--trigger and --where flags are mutually exclusive"))
	}

	if isBulkDeleteMode && (where == "" && len(triggerDeleteIdentifiers) == 0) {
		failOnError(fmt.Errorf("bulk delete mode requires --where or --trigger flags"))
	}

	if err := validateSpaceFlag(isBulkDeleteMode); err != nil {
		failOnError(err)
	}

	return isBulkDeleteMode
}

func runBulkTriggerDelete() error {
	// Build WHERE clause from trigger identifiers or use provided where clause
	var effectiveWhere string
	if len(triggerDeleteIdentifiers) > 0 {
		whereClause, err := buildWhereClauseFromTriggers(triggerDeleteIdentifiers)
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
	params := &goclientnew.BulkDeleteTriggersParams{
		Where:   &effectiveWhere,
		Include: &include,
	}

	// Call the bulk delete API
	bulkRes, err := cubClientNew.BulkDeleteTriggersWithResponse(ctx, params)
	if IsAPIError(err, bulkRes) {
		return InterpretErrorGeneric(err, bulkRes)
	}

	// Handle the response
	return handleBulkTriggerDeleteResponse(bulkRes.JSON200, bulkRes.JSON207, bulkRes.StatusCode(), "delete", effectiveWhere)
}

func triggerDeleteCmdRun(cmd *cobra.Command, args []string) error {
	isBulkDeleteMode := checkTriggerDeleteConflictingArgs(args)

	if isBulkDeleteMode {
		return runBulkTriggerDelete()
	}

	// Single trigger delete logic
	triggerDetails, err := apiGetTriggerFromSlug(args[0], "*") // get all fields for now
	if err != nil {
		return err
	}
	deleteRes, err := cubClientNew.DeleteTriggerWithResponse(ctx, uuid.MustParse(selectedSpaceID), triggerDetails.TriggerID)
	if IsAPIError(err, deleteRes) {
		return InterpretErrorGeneric(err, deleteRes)
	}

	displayDeleteResults("trigger", args[0], triggerDetails.TriggerID.String())
	return nil
}

func handleBulkTriggerDeleteResponse(responses200 *[]goclientnew.DeleteResponse, responses207 *[]goclientnew.DeleteResponse, statusCode int, operationName, contextInfo string) error {
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
				fmt.Printf("Successfully %sd trigger: %s\n", operationName, resp.Message)
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
		fmt.Printf("  Success: %d trigger(s)\n", successCount)
		if failureCount > 0 {
			fmt.Printf("  Failed: %d trigger(s)\n", failureCount)
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
