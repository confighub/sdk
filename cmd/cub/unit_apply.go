// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"errors"
	"time"

	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var unitApplyArgs struct {
	whereClause     string
	dryRun          bool
	unitIdentifiers []string
}

var unitApplyCmd = &cobra.Command{
	Use:   "apply [<unit-slug>]",
	Args:  cobra.MaximumNArgs(1),
	Short: "Apply configuration units to the target",
	Long: `Apply configuration units to the target.

Examples:
  # Apply a single unit by slug
  cub unit apply my-unit

  # Apply multiple specific units
  cub unit apply --space my-space --unit unit1,unit2,unit3
  cub unit apply --space my-space --unit unit1 --unit unit2 --unit unit3

  # Bulk apply units using a WHERE clause with labels
  cub unit apply --space my-space --where "Labels.Tier = 'backend'"

  # Apply units with multiple label conditions
  cub unit apply --space my-space --where "Labels.App = 'api' AND Labels.Tier = 'backend'"

  # Dry run to see what would be applied
  cub unit apply --space my-space --unit unit1,unit2 --dry-run

  # Apply all unapplied units in a space
  cub unit apply --space my-space --where "HeadRevisionNum > LiveRevisionNum"

  # Apply units across all spaces (requires --space "*")
  cub unit apply --space "*" --where "Space.Labels.Environment = 'staging'"`,
	RunE:        unitApplyCmdRun,
	Annotations: map[string]string{"OrgLevel": ""},
}

func init() {
	enableWaitFlag(unitApplyCmd)
	enableQuietFlagForOperation(unitApplyCmd)
	enableJsonFlag(unitApplyCmd)
	unitApplyCmd.Flags().StringVar(&unitApplyArgs.whereClause, "where", "", "WHERE clause to filter units for bulk apply")
	unitApplyCmd.Flags().BoolVar(&unitApplyArgs.dryRun, "dry-run", false, "Perform a dry run without actually applying")
	unitApplyCmd.Flags().StringSliceVar(&unitApplyArgs.unitIdentifiers, "unit", []string{}, "target specific units by slug or UUID (can be repeated or comma-separated)")
	unitCmd.AddCommand(unitApplyCmd)
}

func unitApplyCmdRun(_ *cobra.Command, args []string) error {
	// Determine operation mode based on arguments and flags
	if len(args) == 1 && unitApplyArgs.whereClause == "" && len(unitApplyArgs.unitIdentifiers) == 0 {
		// Single unit mode
		return runSingleUnitApply(args[0])
	} else if len(args) == 0 {
		// Bulk mode
		return runBulkUnitApply()
	} else {
		return errors.New("invalid arguments: use either a single unit slug, --unit flag, or --where flag")
	}
}

func runSingleUnitApply(unitSlug string) error {
	configUnit, err := apiGetUnitFromSlug(unitSlug, "*")
	if err != nil {
		return err
	}

	applyRes, err := cubClientNew.ApplyUnitWithResponse(ctx, uuid.MustParse(selectedSpaceID), configUnit.UnitID)
	if IsAPIError(err, applyRes) {
		return InterpretErrorGeneric(err, applyRes)
	}

	// Handle wait flag
	if wait {
		err = awaitCompletion("apply", applyRes.JSON200)
		if err != nil {
			return err
		}
	}

	// Output JSON if requested
	if jsonOutput {
		displayJSON(applyRes.JSON200)
	}
	if jq != "" {
		displayJQ(applyRes.JSON200)
	}

	return nil
}

func runBulkUnitApply() error {
	// Check for mutual exclusivity between --unit and --where flags
	if len(unitApplyArgs.unitIdentifiers) > 0 && unitApplyArgs.whereClause != "" {
		return errors.New("--unit and --where flags are mutually exclusive")
	}

	// Must have either --unit or --where
	if len(unitApplyArgs.unitIdentifiers) == 0 && unitApplyArgs.whereClause == "" {
		return errors.New("either --unit or --where flag is required for bulk apply")
	}

	// Build WHERE clause from unit identifiers if provided
	var effectiveWhere string
	if len(unitApplyArgs.unitIdentifiers) > 0 {
		whereClause, err := buildWhereClauseFromUnits(unitApplyArgs.unitIdentifiers)
		if err != nil {
			return err
		}
		effectiveWhere = whereClause
	} else {
		effectiveWhere = unitApplyArgs.whereClause
	}

	// Add space constraint to the where clause if not org level
	effectiveWhere = addSpaceIDToWhereClause(effectiveWhere, selectedSpaceID)

	// Build query parameters
	include := "UnitEventID,TargetID,UpstreamUnitID,SpaceID"
	params := &goclientnew.BulkApplyUnitsParams{
		Where:   effectiveWhere,
		Include: &include,
	}
	if unitApplyArgs.dryRun {
		params.DryRun = &unitApplyArgs.dryRun
	}

	// Call the bulk apply endpoint
	resp, err := cubClientNew.BulkApplyUnitsWithResponse(ctx, params)
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
		return errors.New("unexpected response from bulk apply API")
	}

	return handleBulkApplyResponse(responses)
}

func handleBulkApplyResponse(results *[]goclientnew.UnitActionResponse) error {
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

	for _, result := range *results {
		if result.Error != nil {
			failureCount++
			if !quiet {
				// Display error for this unit
				if result.Error.ErrorMetadata != nil && result.Error.ErrorMetadata.EntityID != "" {
					tprint("Failed to apply unit %s: %s", result.Error.ErrorMetadata.EntityID, result.Error.Message)
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
					tprint("Queued apply for unit (%s)", result.Action.UnitID)
				} else {
					tprint("Queued apply for unit %s (%s)", unitDetails.Slug, result.Action.UnitID)
				}
			}
		}
	}

	// Display summary
	if !quiet {
		tprint("") // blank line before summary
		if unitApplyArgs.dryRun {
			tprint("Dry run completed (no changes made)")
			tprint("Units that would be applied: %d", successCount)
			if failureCount > 0 {
				tprint("Units that would fail: %d", failureCount)
			}
		} else {
			tprint("Bulk apply completed")
			tprint("Units queued for apply: %d", successCount)
			if failureCount > 0 {
				tprint("Units failed: %d", failureCount)
			}
		}
		tprint("Total units processed: %d", len(*results))
	}

	// If wait flag is set and not dry run, wait for all operations to complete
	if wait && !unitApplyArgs.dryRun && len(queuedOps) > 0 {
		if !quiet {
			tprint("")
			tprint("Waiting for %d operation(s) to complete...", len(queuedOps))
		}
		for _, op := range queuedOps {
			if err := awaitCompletion("apply", op); err != nil {
				if !quiet {
					tprint("Warning: %v", err)
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

	return nil
}

func actionType(action *goclientnew.ActionType) goclientnew.ActionType {
	if action == nil {
		return "None"
	}
	return *action
}

func actionStatus(status *goclientnew.ActionStatusType) goclientnew.ActionStatusType {
	if status == nil {
		return "None"
	}
	return *status
}

func displayOperationResults(id string, event *goclientnew.UnitEvent) {
	if quiet {
		return
	}
	// Try to get the unit slug for better display
	unitDetails, err := apiGetUnit(id, "Slug")
	if err != nil {
		// Fallback to UUID if we can't get the slug
		if actionStatus(event.Status) == goclientnew.ActionStatusTypeCompleted {
			tprint("Successfully completed %s on unit (%s)", actionType(event.Action), id)
			return
		}
		tprint("Action %s on unit (%s) %s", actionType(event.Action), id, actionStatus(event.Status))
	} else {
		if actionStatus(event.Status) == goclientnew.ActionStatusTypeCompleted {
			tprint("Successfully completed %s on unit %s (%s)", actionType(event.Action), unitDetails.Slug, id)
			return
		}
		tprint("Action %s on unit %s (%s) %s", actionType(event.Action), unitDetails.Slug, id, actionStatus(event.Status))
	}
}

func awaitCompletion(action string, queuedOp *goclientnew.QueuedOperation) error {
	timeoutDuration, err := time.ParseDuration(timeout)
	if err != nil {
		return errors.New("invalid timeout duration " + timeout)
	}
	if queuedOp == nil {
		return errors.New(action + " returned no operation")
	}
	unitID := queuedOp.UnitID
	unitIDString := unitID.String()
	spaceID := queuedOp.SpaceID
	spaceIDString := spaceID.String()
	started := false
	whereQueuedOp := "QueuedOperationID='" + queuedOp.QueuedOperationID.String() + "'"
	done := false
	failed := false
	sleepDuration := 200 * time.Millisecond
	maxSleepDuration := sleepDuration * 32
	startTime := time.Now()
	for time.Since(startTime) < timeoutDuration {
		if !started {
			// Check that the queued operation has posted events
			events, err := apiListUnitEvents(spaceID, unitID, whereQueuedOp)
			if err == nil && len(events) > 0 {
				// tprint(string(*queuedOp.Action) + " started")
				started = true
			}
		} else {
			extendedUnit, err := apiGetExtendedUnitFromSlugInSpace(unitIDString, spaceIDString, "*")
			// if err == nil {
			// 	displayUnitEventList([]*goclientnew.UnitEvent{extendedUnit.LatestUnitEvent})
			// }
			if err == nil && extendedUnit.LatestUnitEvent != nil {
				if extendedUnit.LatestUnitEvent.QueuedOperationID != queuedOp.QueuedOperationID ||
					actionType(extendedUnit.LatestUnitEvent.Action) != actionType(queuedOp.Action) ||
					actionStatus(extendedUnit.LatestUnitEvent.Status) == goclientnew.ActionStatusTypeCompleted ||
					actionStatus(extendedUnit.LatestUnitEvent.Status) == goclientnew.ActionStatusTypeCanceled ||
					actionStatus(extendedUnit.LatestUnitEvent.Status) == goclientnew.ActionStatusTypeFailed {
					done = true
					if actionStatus(extendedUnit.LatestUnitEvent.Status) == goclientnew.ActionStatusTypeFailed {
						failed = true
					}
					break
				}
			}
		}
		time.Sleep(sleepDuration)
		sleepDuration *= 2
		if sleepDuration > maxSleepDuration {
			sleepDuration = maxSleepDuration
		}
	}
	if !done {
		return errors.New(string(*queuedOp.Action) + " didn't complete on unit " + unitIDString)
	}
	if failed {
		return errors.New(string(*queuedOp.Action) + " failed on unit " + unitIDString)
	}
	unitDetails, err := apiGetUnit(unitIDString, "*") // get all fields for now
	if err != nil {
		return err
	}
	err = awaitTriggersRemoval(unitDetails)
	if err != nil {
		return err
	}
	events, err := apiListUnitEvents(spaceID, unitID, whereQueuedOp)
	if err != nil {
		return err
	}
	if len(events) == 0 {
		return errors.New("no matching events found for completed operation")
	}
	// Look for a completion event
	for _, event := range events {
		if actionStatus(event.Status) == goclientnew.ActionStatusTypeCompleted ||
			actionStatus(event.Status) == goclientnew.ActionStatusTypeCanceled ||
			actionStatus(event.Status) == goclientnew.ActionStatusTypeFailed {
			displayOperationResults(unitIDString, event)
			return nil
		}
	}
	return errors.New("no matching events found for completed operation")
}
