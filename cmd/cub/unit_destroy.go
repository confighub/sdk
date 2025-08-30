// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"errors"
	"fmt"
	"time"

	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var unitDestroyArgs struct {
	whereClause     string
	dryRun          bool
	unitIdentifiers []string
}

var unitDestroyCmd = &cobra.Command{
	Use:   "destroy [<unit-slug>]",
	Args:  cobra.MaximumNArgs(1),
	Short: "Destroy configuration units from the target",
	Long: `Destroy configuration units from the target.

Examples:
  # Destroy a single unit by slug
  cub unit destroy my-unit
  
  # Destroy multiple specific units
  cub unit destroy --space my-space --unit unit1,unit2,unit3
  cub unit destroy --space my-space --unit unit1 --unit unit2 --unit unit3

  # Bulk destroy units using a WHERE clause with labels
  cub unit destroy --space my-space --where "Labels.Tier = 'backend'"
  
  # Destroy units with multiple label conditions
  cub unit destroy --space my-space --where "Labels.App = 'api' AND Labels.Tier = 'backend'"

  # Dry run to see what would be destroyed
  cub unit destroy --space my-space --unit unit1,unit2 --dry-run

  # Destroy all applied units in a space (use with caution!)
  cub unit destroy --space my-space --where "LiveRevisionNum > 0"

  # Destroy units across all spaces (requires --space "*")
  cub unit destroy --space "*" --where "Space.Labels.Environment = 'test'"`,
	RunE:        unitDestroyCmdRun,
	Annotations: map[string]string{"OrgLevel": ""},
}

func init() {
	enableWaitFlag(unitDestroyCmd)
	enableQuietFlagForOperation(unitDestroyCmd)
	enableJsonFlag(unitDestroyCmd)
	unitDestroyCmd.Flags().StringVar(&unitDestroyArgs.whereClause, "where", "", "WHERE clause to filter units for bulk destroy")
	unitDestroyCmd.Flags().BoolVar(&unitDestroyArgs.dryRun, "dry-run", false, "Perform a dry run without actually destroying")
	unitDestroyCmd.Flags().StringSliceVar(&unitDestroyArgs.unitIdentifiers, "unit", []string{}, "target specific units by slug or UUID (can be repeated or comma-separated)")
	unitCmd.AddCommand(unitDestroyCmd)
}

func unitDestroyCmdRun(_ *cobra.Command, args []string) error {
	// Determine operation mode based on arguments and flags
	if len(args) == 1 && unitDestroyArgs.whereClause == "" && len(unitDestroyArgs.unitIdentifiers) == 0 {
		// Single unit mode
		return runSingleUnitDestroy(args[0])
	} else if len(args) == 0 {
		// Bulk mode
		return runBulkUnitDestroy()
	} else {
		return errors.New("invalid arguments: use either a single unit slug, --unit flag, or --where flag")
	}
}

func runSingleUnitDestroy(unitSlug string) error {
	configUnit, err := apiGetUnitFromSlug(unitSlug, "*")
	if err != nil {
		return err
	}

	destroyRes, err := cubClientNew.DestroyUnitWithResponse(ctx, uuid.MustParse(selectedSpaceID), configUnit.UnitID)
	if IsAPIError(err, destroyRes) {
		return InterpretErrorGeneric(err, destroyRes)
	}

	// Handle wait flag
	if wait {
		err = awaitCompletion("destroy", destroyRes.JSON200)
		if err != nil {
			return err
		}
	}

	// Output JSON if requested
	if jsonOutput {
		displayJSON(destroyRes.JSON200)
	}
	if jq != "" {
		displayJQ(destroyRes.JSON200)
	}

	return nil
}

func runBulkUnitDestroy() error {
	// Check for mutual exclusivity between --unit and --where flags
	if len(unitDestroyArgs.unitIdentifiers) > 0 && unitDestroyArgs.whereClause != "" {
		return errors.New("--unit and --where flags are mutually exclusive")
	}

	// Must have either --unit or --where
	if len(unitDestroyArgs.unitIdentifiers) == 0 && unitDestroyArgs.whereClause == "" {
		return errors.New("either --unit or --where flag is required for bulk destroy")
	}

	// Build WHERE clause from unit identifiers if provided
	var effectiveWhere string
	if len(unitDestroyArgs.unitIdentifiers) > 0 {
		whereClause, err := buildWhereClauseFromUnits(unitDestroyArgs.unitIdentifiers)
		if err != nil {
			return err
		}
		effectiveWhere = whereClause
	} else {
		effectiveWhere = unitDestroyArgs.whereClause
	}

	// Add space constraint to the where clause if not org level
	effectiveWhere = addSpaceIDToWhereClause(effectiveWhere, selectedSpaceID)

	// Build query parameters
	include := "UnitEventID,TargetID,UpstreamUnitID,SpaceID"
	params := &goclientnew.BulkDestroyUnitsParams{
		Where:   effectiveWhere,
		Include: &include,
	}
	if unitDestroyArgs.dryRun {
		params.DryRun = &unitDestroyArgs.dryRun
	}

	// Call the bulk destroy endpoint
	resp, err := cubClientNew.BulkDestroyUnitsWithResponse(ctx, params)
	if IsAPIError(err, resp) {
		return InterpretErrorGeneric(err, resp)
	}

	// Handle the response - could be 200 (all success) or 207 (mixed results)
	var responses *[]goclientnew.UnitActionResponse
	if resp.JSON200 != nil {
		responses = resp.JSON200
	} else if resp.JSON207 != nil {
		responses = resp.JSON207
	} else {
		return errors.New("unexpected response from bulk destroy API")
	}

	return handleBulkDestroyResponse(responses)
}

func handleBulkDestroyResponse(results *[]goclientnew.UnitActionResponse) error {
	if results == nil || len(*results) == 0 {
		if !quiet {
			tprint("No units found matching the filter")
		}
		if jsonOutput {
			displayJSON(results)
		}
		if jq != "" {
			displayJQ(results)
		}
		return nil
	}

	// Count successes and failures
	var successCount, failureCount int
	var queuedOps []*goclientnew.QueuedOperation
	var hasErrors bool

	for _, result := range *results {
		if result.Error != nil {
			failureCount++
			hasErrors = true
			if !quiet {
				// Display error for this unit
				if result.Error.ErrorMetadata != nil && result.Error.ErrorMetadata.EntityID != "" {
					tprint("Failed to destroy unit %s: %s", result.Error.ErrorMetadata.EntityID, result.Error.Message)
				} else {
					tprint("Failed: %s", result.Error.Message)
				}
			}
		} else if result.Action != nil {
			successCount++
			queuedOps = append(queuedOps, result.Action)
			if !quiet && !wait {
				// Fetch unit details to get the slug
				unitDetails, err := apiGetUnit(result.Action.UnitID.String(), "Slug")
				if err != nil {
					// Fallback to UUID if we can't get the slug
					tprint("Queued destroy for unit (%s)", result.Action.UnitID)
				} else {
					tprint("Queued destroy for unit %s (%s)", unitDetails.Slug, result.Action.UnitID)
				}
			}
		}
	}

	// Display summary
	if !quiet {
		if unitDestroyArgs.dryRun {
			tprint("") // blank line before summary
			tprint("Dry run completed (no changes made)")
			tprint("Units that would be destroyed: %d", successCount)
			if failureCount > 0 {
				tprint("Units that would fail: %d", failureCount)
			}
		} else {
			tprint("Bulk destroy completed")
			tprint("Units queued for destroy: %d", successCount)
			if failureCount > 0 {
				tprint("Units failed: %d", failureCount)
			}
		}
	}

	// Handle wait flag - wait for all successful operations
	if wait && len(queuedOps) > 0 && !unitDestroyArgs.dryRun {
		if !quiet {
			tprint("\nWaiting for destroy operations to complete...")
		}

		// Wait for all operations in parallel
		for _, op := range queuedOps {
			if op != nil {
				// Note: awaitCompletion might need to be adapted for parallel waiting
				// For now, we'll wait sequentially
				err := awaitCompletionWithTimeout("destroy", op, 5*time.Minute)
				if err != nil {
					hasErrors = true
					if !quiet {
						tprint("Destroy operation failed for unit %s: %v", op.UnitID, err)
					}
				}
			}
		}
	}

	// Output JSON if requested
	if jsonOutput {
		displayJSON(results)
	}
	if jq != "" {
		displayJQ(results)
	}

	// Return error if there were any failures
	if hasErrors && !unitDestroyArgs.dryRun {
		return fmt.Errorf("some units failed to destroy: %d succeeded, %d failed", successCount, failureCount)
	}

	return nil
}

// Helper function with timeout for await completion
func awaitCompletionWithTimeout(operation string, op *goclientnew.QueuedOperation, timeout time.Duration) error {
	if op == nil {
		return errors.New("no queued operation to wait for")
	}
	// This function would need to be implemented based on the existing awaitCompletion function
	// For now, using the standard awaitCompletion
	return awaitCompletion(operation, op)
}
