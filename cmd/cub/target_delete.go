// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"

	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var targetDeleteCmd = &cobra.Command{
	Use:   "delete [<slug or id>]",
	Short: "Delete a target or multiple targets",
	Long: `Delete a target or multiple targets using bulk operations.

Single target delete:
  cub target delete my-target

Bulk delete with --where:
Delete multiple targets at once based on search criteria.

Examples:
  # Delete targets for specific toolchain
  cub target delete --where "ToolchainType = 'Kubernetes/YAML'"

  # Delete targets across all spaces (requires --space "*")
  cub target delete --space "*" --where "Labels.cleanup = 'true'"

  # Delete specific targets by slug
  cub target delete --target my-target,another-target`,
	Args:        cobra.MaximumNArgs(1), // Allow 0 or 1 args (0 for bulk mode)
	RunE:        targetDeleteCmdRun,
	Annotations: map[string]string{"OrgLevel": ""},
}

var (
	targetDeleteIdentifiers []string
)

func init() {
	addStandardDeleteFlags(targetDeleteCmd)
	enableWhereFlag(targetDeleteCmd)
	targetDeleteCmd.Flags().StringSliceVar(&targetDeleteIdentifiers, "target", []string{}, "target specific targets by slug or UUID for bulk delete (can be repeated or comma-separated)")
	targetCmd.AddCommand(targetDeleteCmd)
}

func checkTargetDeleteConflictingArgs(args []string) bool {
	// Check for bulk delete mode (no positional args with --where or --target)
	isBulkDeleteMode := len(args) == 0 && (where != "" || len(targetDeleteIdentifiers) > 0)

	if !isBulkDeleteMode && (where != "" || len(targetDeleteIdentifiers) > 0) {
		failOnError(fmt.Errorf("--where or --target can only be specified with no positional arguments"))
	}

	// Single delete mode validation
	if !isBulkDeleteMode && len(args) != 1 {
		failOnError(fmt.Errorf("single target delete requires exactly one argument: <slug or id>"))
	}

	// Check for mutual exclusivity between --target and --where flags
	if len(targetDeleteIdentifiers) > 0 && where != "" {
		failOnError(fmt.Errorf("--target and --where flags are mutually exclusive"))
	}

	if isBulkDeleteMode && (where == "" && len(targetDeleteIdentifiers) == 0) {
		failOnError(fmt.Errorf("bulk delete mode requires --where or --target flags"))
	}

	return isBulkDeleteMode
}

func buildWhereClauseFromTargets(targetIds []string) (string, error) {
	return buildWhereClauseFromIdentifiers(targetIds, "TargetID", "Slug")
}

func runBulkTargetDelete() error {
	// Build WHERE clause from target identifiers or use provided where clause
	var effectiveWhere string
	if len(targetDeleteIdentifiers) > 0 {
		whereClause, err := buildWhereClauseFromTargets(targetDeleteIdentifiers)
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
	params := &goclientnew.BulkDeleteTargetsParams{
		Where:   &effectiveWhere,
		Include: &include,
	}

	// Call the bulk delete API
	bulkRes, err := cubClientNew.BulkDeleteTargetsWithResponse(ctx, params)
	if IsAPIError(err, bulkRes) {
		return InterpretErrorGeneric(err, bulkRes)
	}

	// Handle the response
	return handleBulkTargetDeleteResponse(bulkRes.JSON200, bulkRes.JSON207, bulkRes.StatusCode(), "delete", effectiveWhere)
}

func targetDeleteCmdRun(cmd *cobra.Command, args []string) error {
	isBulkDeleteMode := checkTargetDeleteConflictingArgs(args)

	if isBulkDeleteMode {
		return runBulkTargetDelete()
	}

	// Single target delete logic
	targetDetails, err := apiGetTargetFromSlug(args[0], selectedSpaceID, "*") // get all fields for now
	if err != nil {
		return err
	}

	deleteRes, err := cubClientNew.DeleteTargetWithResponse(ctx, uuid.MustParse(selectedSpaceID), targetDetails.Target.TargetID)
	if IsAPIError(err, deleteRes) {
		return InterpretErrorGeneric(err, deleteRes)
	}
	displayDeleteResults("target", args[0], targetDetails.Target.TargetID.String())
	return nil
}

func handleBulkTargetDeleteResponse(responses200 *[]goclientnew.DeleteResponse, responses207 *[]goclientnew.DeleteResponse, statusCode int, operationName, contextInfo string) error {
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
				fmt.Printf("Successfully %sd target: %s\n", operationName, resp.Message)
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
		fmt.Printf("  Success: %d target(s)\n", successCount)
		if failureCount > 0 {
			fmt.Printf("  Failed: %d target(s)\n", failureCount)
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
