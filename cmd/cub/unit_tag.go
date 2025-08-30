// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"

	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var (
	tagRevision string
)

var unitTagCmd = &cobra.Command{
	Use:   "tag <tag-slug-or-id>",
	Short: "Add or remove tags to/from unit revisions (supports space/tag syntax)",
	Long: `Add or remove tags to/from unit revisions using bulk operations.

This command allows you to tag specific revisions of units with a tag identifier.
Use bulk selection options to tag multiple units at once.

Tag a specific revision type:
  --revision HeadRevisionNum     Tag the head revision (default)
  --revision LiveRevisionNum     Tag the live revision
  --revision LastAppliedRevisionNum  Tag the last applied revision
  --revision PreviousLiveRevisionNum Tag the previous live revision
  --revision Remove              Remove the tag from the revision
  --revision -                   Remove the tag (shorthand for Remove)

Examples:
  # Tag head revision of a single unit (uses current space)
  cub unit tag my-tag --unit my-unit

  # Tag live revision of all units with specific label
  cub unit tag release-v1 --revision LiveRevisionNum --where "Labels.version = 'v1'"

  # Remove tag from units
  cub unit tag my-tag --revision Remove --unit my-unit,another-unit

  # Remove tag using shorthand
  cub unit tag my-tag --revision - --where "Labels.cleanup = 'true'"

  # Tag units across all spaces (requires --space "*") with space/tag syntax
  cub unit tag production/prod-release --space "*" --where "Labels.env = 'production'"

  # Use space/tag syntax to target tag in specific space
  cub unit tag dev-space/dev-tag --unit my-unit`,
	Args:        cobra.ExactArgs(1), // Require tag slug or ID
	RunE:        unitTagCmdRun,
	Annotations: map[string]string{"OrgLevel": ""},
}

func init() {
	unitTagCmd.Flags().StringVar(&tagRevision, "revision", "HeadRevisionNum", 
		"Which revision to tag: HeadRevisionNum, LiveRevisionNum, LastAppliedRevisionNum, PreviousLiveRevisionNum, Remove, or -")
	enableWhereFlag(unitTagCmd)
	enableFilterFlag(unitTagCmd)
	unitTagCmd.Flags().StringSliceVar(&unitIdentifiers, "unit", []string{}, 
		"target specific units by slug or UUID for bulk tag (can be repeated or comma-separated)")
	unitCmd.AddCommand(unitTagCmd)
}

func checkUnitTagConflictingArgs(args []string) error {
	// Check for mutual exclusivity between --unit and --where flags
	if len(unitIdentifiers) > 0 && where != "" {
		return fmt.Errorf("--unit and --where flags are mutually exclusive")
	}

	// At least one selection method is required
	if len(unitIdentifiers) == 0 && where == "" && filter == "" {
		return fmt.Errorf("must specify --unit, --where, or --filter to select units")
	}

	// Validate revision flag value
	switch tagRevision {
	case "HeadRevisionNum", "LiveRevisionNum", "LastAppliedRevisionNum", 
		 "PreviousLiveRevisionNum", "Remove", "-":
		// Valid values
	default:
		return fmt.Errorf("invalid --revision value: %s", tagRevision)
	}

	// Check space flag
	isBulkMode := true // Always bulk mode for tag operation
	if err := validateSpaceFlag(isBulkMode); err != nil {
		return err
	}

	return nil
}

func unitTagCmdRun(cmd *cobra.Command, args []string) error {
	if err := checkUnitTagConflictingArgs(args); err != nil {
		return err
	}

	// Parse the tag argument (supports space/tag format)
	tagSlugOrID := args[0]
	tagID, err := parseTagSlug(tagSlugOrID)
	if err != nil {
		return fmt.Errorf("failed to parse tag: %w", err)
	}

	// Convert "-" to "Remove" for the API
	revision := tagRevision
	if revision == "-" {
		revision = "Remove"
	}

	// Parse filter parameter
	filterID, err := parseFilterFlag(filter)
	if err != nil {
		return err
	}

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

	// Build bulk tag parameters
	params := &goclientnew.BulkTagUnitsParams{
		Where: &effectiveWhere,
	}
	if filterID != "" {
		params.Filter = &filterID
	}

	// Parse TagID from string
	parsedTagID, err := uuid.Parse(tagID)
	if err != nil {
		return fmt.Errorf("invalid tag ID format: %w", err)
	}

	// Build request body
	body := goclientnew.UnitTagRequest{
		TagID:    parsedTagID,
		Revision: revision,
	}

	// Call the bulk tag API
	bulkRes, err := cubClientNew.BulkTagUnitsWithResponse(ctx, params, body)
	if IsAPIError(err, bulkRes) {
		return InterpretErrorGeneric(err, bulkRes)
	}

	// Handle the response
	return handleBulkUnitTagResponse(bulkRes.JSON200, bulkRes.JSON207, bulkRes.StatusCode(), revision, tagSlugOrID, effectiveWhere)
}

func handleBulkUnitTagResponse(responses200 *[]goclientnew.UnitTagResponse, responses207 *[]goclientnew.UnitTagResponse, 
	statusCode int, revision, tagIdentifier, contextInfo string) error {
	var responses *[]goclientnew.UnitTagResponse
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
				fmt.Printf("%s\n", resp.Message)
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
		var operation string
		if revision == "Remove" || revision == "-" {
			operation = "Tag removal"
		} else {
			operation = "Tagging"
		}
		
		fmt.Printf("\n%s operation completed for tag '%s':\n", operation, tagIdentifier)
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
		if revision != "Remove" && revision != "-" {
			fmt.Printf("  Revision type: %s\n", revision)
		}
		if contextInfo != "" && verbose {
			fmt.Printf("  Context: %s\n", contextInfo)
		}
	}

	// Return success only if all operations succeeded
	if statusCode == 207 || failureCount > 0 {
		return fmt.Errorf("bulk tag operation partially failed: %d succeeded, %d failed", successCount, failureCount)
	}

	return nil
}

