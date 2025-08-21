// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"

	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var changesetDeleteCmd = &cobra.Command{
	Use:   "delete [<slug or id>]",
	Short: "Delete a changeset or multiple changesets",
	Long: `Delete a changeset or multiple changesets using bulk operations.

Single changeset delete:
  cub changeset delete my-changeset

Bulk delete with --where:
Delete multiple changesets at once based on search criteria.

Examples:
  # Delete all changesets created before a specific date
  cub changeset delete --where "CreatedAt < '2024-01-01'"

  # Delete changesets with specific descriptions
  cub changeset delete --where "Description LIKE '%deprecated%'"

  # Delete changesets across all spaces (requires --space "*")
  cub changeset delete --space "*" --where "Labels.cleanup = 'true'"

  # Delete specific changesets by slug
  cub changeset delete --changeset old-changeset,deprecated-changeset`,
	Args:        cobra.MaximumNArgs(1), // Allow 0 or 1 args (0 for bulk mode)
	RunE:        changesetDeleteCmdRun,
	Annotations: map[string]string{"OrgLevel": ""},
}

var (
	changesetDeleteIdentifiers []string
)

func init() {
	addStandardDeleteFlags(changesetDeleteCmd)
	enableWhereFlag(changesetDeleteCmd)
	changesetDeleteCmd.Flags().StringSliceVar(&changesetDeleteIdentifiers, "changeset", []string{}, "target specific changesets by slug or UUID for bulk delete (can be repeated or comma-separated)")
	changesetCmd.AddCommand(changesetDeleteCmd)
}

func checkChangeSetDeleteConflictingArgs(args []string) bool {
	// Check for bulk delete mode (no positional args with --where or --changeset)
	isBulkDeleteMode := len(args) == 0 && (where != "" || len(changesetDeleteIdentifiers) > 0)

	if !isBulkDeleteMode && (where != "" || len(changesetDeleteIdentifiers) > 0) {
		failOnError(fmt.Errorf("--where or --changeset can only be specified with no positional arguments"))
	}

	// Single delete mode validation
	if !isBulkDeleteMode && len(args) != 1 {
		failOnError(fmt.Errorf("single changeset delete requires exactly one argument: <slug or id>"))
	}

	// Check for mutual exclusivity between --changeset and --where flags
	if len(changesetDeleteIdentifiers) > 0 && where != "" {
		failOnError(fmt.Errorf("--changeset and --where flags are mutually exclusive"))
	}

	if isBulkDeleteMode && (where == "" && len(changesetDeleteIdentifiers) == 0) {
		failOnError(fmt.Errorf("bulk delete mode requires --where or --changeset flags"))
	}

	if err := validateSpaceFlag(isBulkDeleteMode); err != nil {
		failOnError(err)
	}

	return isBulkDeleteMode
}

func runBulkChangeSetDelete() error {
	// Build WHERE clause from changeset identifiers or use provided where clause
	var effectiveWhere string
	if len(changesetDeleteIdentifiers) > 0 {
		whereClause, err := buildWhereClauseFromChangeSets(changesetDeleteIdentifiers)
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
	params := &goclientnew.BulkDeleteChangeSetsParams{
		Where:   &effectiveWhere,
		Include: &include,
	}

	// Call the bulk delete API
	bulkRes, err := cubClientNew.BulkDeleteChangeSetsWithResponse(ctx, params)
	if IsAPIError(err, bulkRes) {
		return InterpretErrorGeneric(err, bulkRes)
	}

	// Handle the response
	return handleBulkChangeSetDeleteResponse(bulkRes.JSON200, bulkRes.JSON207, bulkRes.StatusCode(), "delete", effectiveWhere)
}

func changesetDeleteCmdRun(cmd *cobra.Command, args []string) error {
	isBulkDeleteMode := checkChangeSetDeleteConflictingArgs(args)

	if isBulkDeleteMode {
		return runBulkChangeSetDelete()
	}

	// Single changeset delete logic
	changesetDetails, err := apiGetChangeSetFromSlug(args[0], "*") // get all fields for now
	if err != nil {
		return err
	}
	deleteRes, err := cubClientNew.DeleteChangeSetWithResponse(ctx, uuid.MustParse(selectedSpaceID), changesetDetails.ChangeSetID)
	if IsAPIError(err, deleteRes) {
		return InterpretErrorGeneric(err, deleteRes)
	}

	displayDeleteResults("changeset", args[0], changesetDetails.ChangeSetID.String())
	return nil
}

func handleBulkChangeSetDeleteResponse(responses200 *[]goclientnew.DeleteResponse, responses207 *[]goclientnew.DeleteResponse, statusCode int, operationName, contextInfo string) error {
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
				fmt.Printf("Successfully %sd changeset: %s\n", operationName, resp.Message)
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
		fmt.Printf("  Success: %d changeset(s)\n", successCount)
		if failureCount > 0 {
			fmt.Printf("  Failed: %d changeset(s)\n", failureCount)
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
