// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"

	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var tagDeleteCmd = &cobra.Command{
	Use:   "delete [<slug or id>]",
	Short: "Delete a tag or multiple tags",
	Long: `Delete a tag or multiple tags using bulk operations.

Single tag delete:
  cub tag delete my-tag

Bulk delete with --where:
Delete multiple tags at once based on search criteria.

Examples:
  # Delete all tags created before a specific date
  cub tag delete --where "CreatedAt < '2024-01-01'"

  # Delete tags with specific labels
  cub tag delete --where "Labels.archived = 'true'"

  # Delete tags across all spaces (requires --space "*")
  cub tag delete --space "*" --where "Labels.cleanup = 'true'"

  # Delete specific tags by slug
  cub tag delete --tag old-tag,deprecated-tag`,
	Args:        cobra.MaximumNArgs(1), // Allow 0 or 1 args (0 for bulk mode)
	RunE:        tagDeleteCmdRun,
	Annotations: map[string]string{"OrgLevel": ""},
}

var (
	tagDeleteIdentifiers []string
)

func init() {
	addStandardDeleteFlags(tagDeleteCmd)
	enableWhereFlag(tagDeleteCmd)
	tagDeleteCmd.Flags().StringSliceVar(&tagDeleteIdentifiers, "tag", []string{}, "target specific tags by slug or UUID for bulk delete (can be repeated or comma-separated)")
	tagCmd.AddCommand(tagDeleteCmd)
}

func checkTagDeleteConflictingArgs(args []string) bool {
	// Check for bulk delete mode (no positional args with --where or --tag)
	isBulkDeleteMode := len(args) == 0 && (where != "" || len(tagDeleteIdentifiers) > 0)

	if !isBulkDeleteMode && (where != "" || len(tagDeleteIdentifiers) > 0) {
		failOnError(fmt.Errorf("--where or --tag can only be specified with no positional arguments"))
	}

	// Single delete mode validation
	if !isBulkDeleteMode && len(args) != 1 {
		failOnError(fmt.Errorf("single tag delete requires exactly one argument: <slug or id>"))
	}

	// Check for mutual exclusivity between --tag and --where flags
	if len(tagDeleteIdentifiers) > 0 && where != "" {
		failOnError(fmt.Errorf("--tag and --where flags are mutually exclusive"))
	}

	if isBulkDeleteMode && (where == "" && len(tagDeleteIdentifiers) == 0) {
		failOnError(fmt.Errorf("bulk delete mode requires --where or --tag flags"))
	}

	if err := validateSpaceFlag(isBulkDeleteMode); err != nil {
		failOnError(err)
	}

	return isBulkDeleteMode
}

func runBulkTagDelete() error {
	// Build WHERE clause from tag identifiers or use provided where clause
	var effectiveWhere string
	if len(tagDeleteIdentifiers) > 0 {
		whereClause, err := buildWhereClauseFromTags(tagDeleteIdentifiers)
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
	include := "SpaceID"
	params := &goclientnew.BulkDeleteTagsParams{
		Where:   &effectiveWhere,
		Include: &include,
	}

	// Call the bulk delete API
	bulkRes, err := cubClientNew.BulkDeleteTagsWithResponse(ctx, params)
	if IsAPIError(err, bulkRes) {
		return InterpretErrorGeneric(err, bulkRes)
	}

	// Handle the response
	return handleBulkTagDeleteResponse(bulkRes.JSON200, bulkRes.JSON207, bulkRes.StatusCode(), "delete", effectiveWhere)
}

func tagDeleteCmdRun(cmd *cobra.Command, args []string) error {
	isBulkDeleteMode := checkTagDeleteConflictingArgs(args)

	if isBulkDeleteMode {
		return runBulkTagDelete()
	}

	// Single tag delete logic
	tagDetails, err := apiGetTagFromSlug(args[0], "*") // get all fields for now
	if err != nil {
		return err
	}
	deleteRes, err := cubClientNew.DeleteTagWithResponse(ctx, uuid.MustParse(selectedSpaceID), tagDetails.TagID)
	if IsAPIError(err, deleteRes) {
		return InterpretErrorGeneric(err, deleteRes)
	}

	displayDeleteResults("tag", args[0], tagDetails.TagID.String())
	return nil
}

func handleBulkTagDeleteResponse(responses200 *[]goclientnew.DeleteResponse, responses207 *[]goclientnew.DeleteResponse, statusCode int, operationName, contextInfo string) error {
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
				fmt.Printf("Successfully %sd tag: %s\n", operationName, resp.Message)
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
		fmt.Printf("  Success: %d tag(s)\n", successCount)
		if failureCount > 0 {
			fmt.Printf("  Failed: %d tag(s)\n", failureCount)
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
