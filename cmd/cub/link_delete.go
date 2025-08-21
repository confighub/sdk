// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"

	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var linkDeleteCmd = &cobra.Command{
	Use:   "delete [<slug or id>]",
	Short: "Delete a link or multiple links",
	Long: `Delete a link or multiple links using bulk operations.

Single link delete:
  cub link delete my-link

Bulk delete with --where:
Delete multiple links at once based on search criteria.

Examples:
  # Delete all links to a specific unit
  cub link delete --where "ToUnitID = 'unit-uuid'"

  # Delete cross-space links
  cub link delete --where "ToSpaceID != SpaceID"

  # Delete links across all spaces (requires --space "*")
  cub link delete --space "*" --where "Labels.cleanup = 'true'"

  # Delete specific links by slug
  cub link delete --link my-link,another-link`,
	Args:        cobra.MaximumNArgs(1), // Allow 0 or 1 args (0 for bulk mode)
	RunE:        linkDeleteCmdRun,
	Annotations: map[string]string{"OrgLevel": ""},
}

var (
	linkDeleteIdentifiers []string
)

func init() {
	addStandardDeleteFlags(linkDeleteCmd)
	enableWaitFlag(linkDeleteCmd)
	enableWhereFlag(linkDeleteCmd)
	linkDeleteCmd.Flags().StringSliceVar(&linkDeleteIdentifiers, "link", []string{}, "target specific links by slug or UUID for bulk delete (can be repeated or comma-separated)")
	linkCmd.AddCommand(linkDeleteCmd)
}

func handleBulkLinkDeleteResponse(responses200 *[]goclientnew.DeleteResponse, responses207 *[]goclientnew.DeleteResponse, statusCode int, operationName, contextInfo string) error {
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
				fmt.Printf("Successfully %sd link: %s\n", operationName, resp.Message)
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
		fmt.Printf("  Success: %d link(s)\n", successCount)
		if failureCount > 0 {
			fmt.Printf("  Failed: %d link(s)\n", failureCount)
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

func checkLinkDeleteConflictingArgs(args []string) bool {
	// Check for bulk delete mode
	isBulkDeleteMode := len(args) == 0 && (where != "" || len(linkDeleteIdentifiers) > 0)

	if !isBulkDeleteMode && (where != "" || len(linkDeleteIdentifiers) > 0) {
		fmt.Printf("Error: --where and --link flags can only be used in bulk mode (without positional arguments)\n")
		return false
	}

	if err := validateSpaceFlag(isBulkDeleteMode); err != nil {
		failOnError(err)
	}

	return isBulkDeleteMode
}

func runBulkLinkDelete() error {
	var effectiveWhere string
	if len(linkDeleteIdentifiers) > 0 {
		whereClause, err := buildWhereClauseFromLinks(linkDeleteIdentifiers)
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
	include := "SpaceID,FromUnitID,ToUnitID,ToSpaceID"
	params := &goclientnew.BulkDeleteLinksParams{
		Where:   &effectiveWhere,
		Include: &include,
	}
	if contains != "" {
		params.Contains = &contains
	}

	res, err := cubClientNew.BulkDeleteLinksWithResponse(ctx, params)
	if IsAPIError(err, res) {
		return InterpretErrorGeneric(err, res)
	}

	// Handle the response
	return handleBulkLinkDeleteResponse(res.JSON200, res.JSONDefault, res.StatusCode(), "delete", effectiveWhere)
}

func linkDeleteCmdRun(cmd *cobra.Command, args []string) error {
	isBulkDeleteMode := checkLinkDeleteConflictingArgs(args)

	if isBulkDeleteMode {
		return runBulkLinkDelete()
	}

	// Single link delete logic
	if len(args) != 1 {
		return fmt.Errorf("specify a link slug/id for single delete, or use --where/--link for bulk delete")
	}

	linkDetails, err := apiGetLinkFromSlug(args[0], "") // default select is fine
	if err != nil {
		return err
	}

	linkRes, err := cubClientNew.DeleteLinkWithResponse(ctx, uuid.MustParse(selectedSpaceID), linkDetails.LinkID)
	if IsAPIError(err, linkRes) {
		return InterpretErrorGeneric(err, linkRes)
	}
	displayDeleteResults("link", args[0], linkDetails.LinkID.String())
	if wait {
		if !quiet {
			tprint("Awaiting triggers...")
		}
		fromUnitID := linkDetails.FromUnitID
		unitDetails, err := apiGetUnit(fromUnitID.String(), "*") // get all fields for now
		if err != nil {
			return err
		}
		err = awaitTriggersRemoval(unitDetails)
		if err != nil {
			return err
		}
	}
	return nil
}
