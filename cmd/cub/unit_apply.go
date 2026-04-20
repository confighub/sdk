// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var unitApplyArgs struct {
	dryRun          bool
	unitIdentifiers []string
	revision        string
	driftMode       string
}

var unitApplyCmd = &cobra.Command{
	Use:   "apply [<unit-slug>]",
	Args:  cobra.MaximumNArgs(1),
	Short: "Apply configuration units to the target",
	Long: getCommandHelp(`Apply configuration units to the target.

A target must be attached to the unit (cub unit set-target can be used to add one), and the
worker corresponding to the target must be connected and ready (cub worker get can be used to
get the worker status).

Examples:
`+"```"+`
  # Apply a single unit by slug
  cub unit apply my-unit

  # Apply a specific revision
  cub unit apply my-unit --revision 5
  cub unit apply my-unit --revision LiveRevisionNum
  cub unit apply my-unit --revision Tag:release-v1.0

  # Apply multiple specific units
  cub unit apply --space my-space --unit unit1,unit2,unit3
  cub unit apply --space my-space --unit unit1 --unit unit2 --unit unit3

  # Bulk apply units using a WHERE clause with labels
  cub unit apply --space my-space --where "Labels.Tier = 'backend'"

  # Apply specific revision for multiple units
  cub unit apply --space my-space --where "Labels.Tier = 'backend'" --revision LiveRevisionNum

  # Apply units with multiple label conditions
  cub unit apply --space my-space --where "Labels.App = 'api' AND Labels.Tier = 'backend'"

  # Dry run to see what would be applied
  cub unit apply --space my-space --unit unit1,unit2 --dry-run

  # Apply all unapplied units in a space
  cub unit apply --space my-space --where "HeadRevisionNum > LiveRevisionNum"

  # Apply units across all spaces (requires --space "*")
  cub unit apply --space "*" --where "Space.Labels.Environment = 'staging'"
`+"```"+`
`, ""),
	RunE:        unitApplyCmdRun,
	Annotations: map[string]string{"OrgLevel": ""},
}

func init() {
	enableActionWaitFlag(unitApplyCmd)
	addStandardDisplayFlags(unitApplyCmd)
	enableWhereFlag(unitApplyCmd)
	enableFilterFlag(unitApplyCmd)
	unitApplyCmd.Flags().BoolVar(&unitApplyArgs.dryRun, "dry-run", false, "Perform a dry run without actually applying")
	unitApplyCmd.Flags().StringSliceVar(&unitApplyArgs.unitIdentifiers, "unit", []string{}, "target specific units by slug or UUID (can be repeated or comma-separated)")
	unitApplyCmd.Flags().StringVar(&unitApplyArgs.revision, "revision", "", "Revision to apply (defaults to HeadRevisionNum). Can be a revision number, 'LiveRevisionNum', 'LastAppliedRevisionNum', 'Tag:slug', 'ChangeSet:slug', etc.")
	unitApplyCmd.Flags().StringVar(&unitApplyArgs.driftMode, "drift-mode", "", "Drift reconciliation mode (OnDemand, ContinuousApply, ContinuousRefresh)")
	unitCmd.AddCommand(unitApplyCmd)
}

// parseApplyRevisionParameter parses the revision parameter for apply operations
// and returns the properly formatted revision string for the API.
func parseApplyRevisionParameter(revision string) (*string, error) {
	if revision == "" {
		return nil, nil
	}

	// Check for Before: prefix (not supported for apply)
	if strings.HasPrefix(revision, "Before:") {
		return nil, fmt.Errorf("'Before:' modifier is not supported for apply operations")
	}

	// Parse the revision parameter
	parts := strings.Split(revision, ":")
	var entityType, identifier string

	switch len(parts) {
	case 2:
		// EntityType:Identifier format
		entityType = parts[0]
		identifier = parts[1]
	case 1:
		// Simple identifier (LiveRevisionNum, UUID, integer, etc.)
		identifier = parts[0]
	default:
		return nil, fmt.Errorf("invalid revision specification: %s", revision)
	}

	// Handle entity type-specific parsing
	if entityType == "Tag" {
		// Parse tag slug/ID and convert to UUID
		tagUUID, err := parseTagSlug(identifier)
		if err != nil {
			return nil, fmt.Errorf("failed to parse tag '%s': %w", identifier, err)
		}
		result := fmt.Sprintf("Tag:%s", tagUUID.String())
		return &result, nil

	} else if entityType == "ChangeSet" {
		// Parse changeset slug/ID and convert to UUID
		changesetUUID, err := parseChangeSetSlug(identifier)
		if err != nil {
			return nil, fmt.Errorf("failed to parse changeset '%s': %w", identifier, err)
		}
		result := fmt.Sprintf("ChangeSet:%s", changesetUUID)
		return &result, nil

	} else if entityType == "Revision" {
		// Handle Revision:uuid format
		if _, err := uuid.Parse(identifier); err != nil {
			return nil, fmt.Errorf("invalid revision UUID: %s", identifier)
		}
		result := fmt.Sprintf("Revision:%s", identifier)
		return &result, nil

	} else if entityType != "" {
		return nil, fmt.Errorf("unsupported entity type '%s': supported types are Tag, ChangeSet, and Revision", entityType)
	}

	// Handle simple identifiers (no entity type prefix)
	namedRevisions := map[string]bool{
		"LiveRevisionNum":         true,
		"LastAppliedRevisionNum":  true,
		"PreviousLiveRevisionNum": true,
		"HeadRevisionNum":         true,
	}

	if namedRevisions[identifier] {
		// Named revisions are passed as-is
		return &identifier, nil
	}

	// Check if it's a UUID
	if _, err := uuid.Parse(identifier); err == nil {
		// It's a UUID - format as Revision:uuid
		result := fmt.Sprintf("Revision:%s", identifier)
		return &result, nil
	}

	// Check if it's a number (revision number)
	if _, err := strconv.ParseInt(identifier, 10, 64); err == nil {
		// It's a number - pass as-is (the API will handle it)
		return &identifier, nil
	}

	// Not a valid revision specification
	return nil, fmt.Errorf("invalid revision specification: %s. Must be a revision number, named revision (LiveRevisionNum, LastAppliedRevisionNum, PreviousLiveRevisionNum, HeadRevisionNum), UUID, or EntityType:identifier format", revision)
}

func unitApplyCmdRun(_ *cobra.Command, args []string) error {
	if len(args) == 0 {
		return runBulkUnitApply()
	}

	return runSingleUnitApply(args[0])
}

func runSingleUnitApply(unitSlug string) error {
	configUnit, err := apiGetUnitFromSlug(unitSlug, "*")
	if err != nil {
		return err
	}

	// Parse revision parameter
	revisionParam, err := parseApplyRevisionParameter(unitApplyArgs.revision)
	if err != nil {
		return err
	}

	// Build apply parameters
	params := &goclientnew.ApplyUnitParams{}
	if unitApplyArgs.dryRun {
		params.DryRun = &unitApplyArgs.dryRun
	}
	if revisionParam != nil {
		params.Revision = revisionParam
	}
	if unitApplyArgs.driftMode != "" {
		params.DriftMode = &unitApplyArgs.driftMode
	}

	applyRes, err := cubClientNew.ApplyUnitWithResponse(ctx, uuid.MustParse(selectedSpaceID), configUnit.UnitID, params)
	if cubapi.IsAPIError(err, applyRes) {
		return cubapi.InterpretErrorGeneric(err, applyRes)
	}

	// Handle wait flag
	if actionWait {
		err = awaitCompletion("apply", applyRes.JSON200)
		if err != nil {
			return err
		}
	} else if !quiet && !isAlternativeOutput() {
		displayStartedOperation(applyRes.JSON200)
		return nil
	}

	renderPayload(applyRes.JSON200)

	return nil
}

func runBulkUnitApply() error {
	// Check for mutual exclusivity between --unit and --where flags
	if len(unitApplyArgs.unitIdentifiers) > 0 && where != "" {
		return errors.New("--unit and --where flags are mutually exclusive")
	}

	// Parse revision parameter
	revisionParam, err := parseApplyRevisionParameter(unitApplyArgs.revision)
	if err != nil {
		return err
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
		effectiveWhere = where
	}

	// Add space constraint to the where clause if not org level
	effectiveWhere = addSpaceIDToWhereClause(effectiveWhere, selectedSpaceID)

	// Parse filter parameter
	filterID, err := parseFilterFlag(filter)
	if err != nil {
		return err
	}

	// Build query parameters
	include := "UnitEventID,TargetID,UpstreamUnitID,SpaceID"
	params := &goclientnew.BulkApplyUnitsParams{
		Where:   effectiveWhere,
		Include: &include,
	}
	if filterID != "" {
		params.Filter = &filterID
	}
	if unitApplyArgs.dryRun {
		params.DryRun = &unitApplyArgs.dryRun
	}
	if revisionParam != nil {
		params.Revision = revisionParam
	}
	if unitApplyArgs.driftMode != "" {
		params.DriftMode = &unitApplyArgs.driftMode
	}

	// Call the bulk apply endpoint
	resp, err := cubClientNew.BulkApplyUnitsWithResponse(ctx, params)
	if cubapi.IsAPIError(err, resp) {
		return cubapi.InterpretErrorGeneric(err, resp)
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

	return handleBulkUnitActionResponse(responses, "apply", unitApplyArgs.dryRun)
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

// isTerminalStatus returns true if the status represents a terminal state
// (Completed, Canceled, Failed, or Aborted)
func isTerminalStatus(status goclientnew.ActionStatusType) bool {
	return status == goclientnew.ActionStatusTypeCompleted ||
		status == goclientnew.ActionStatusTypeCanceled ||
		status == goclientnew.ActionStatusTypeFailed ||
		status == goclientnew.ActionStatusTypeAborted
}

func displayStartedOperation(queuedOperation *goclientnew.QueuedOperation) {
	unitID := queuedOperation.UnitID.String()
	spaceID := queuedOperation.SpaceID.String()

	// Try to get the unit slug for better display
	// Fallback to UUID if we can't get the slug
	slug := unitID
	unitDetails, err := apiGetUnitInSpace(unitID, spaceID, "Slug")
	if err == nil {
		slug = unitDetails.Slug
	}
	tprint("Action %s on unit %s started", actionType(queuedOperation.Action), slug)
}

func displayOperationResults(id string, event *goclientnew.UnitEvent) {
	if quiet {
		return
	}
	// Try to get the unit slug for better display
	unitDetails, err := apiGetUnitInSpace(id, event.SpaceID.String(), "Slug")
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
	if queuedOp == nil {
		return errors.New(action + " returned no operation")
	}

	// Use bulk completion for the actual polling
	results := awaitBulkCompletion(action, []*goclientnew.QueuedOperation{queuedOp})

	// Extract result for this single operation
	opID := queuedOp.QueuedOperationID.String()
	if err := results[opID]; err != nil {
		return err
	}

	// Wait for triggers (separate from action wait)
	unitDetails, err := apiGetUnitInSpace(queuedOp.UnitID.String(), queuedOp.SpaceID.String(), "*")
	if err != nil {
		return err
	}
	return awaitTriggersRemoval(unitDetails)
}

// awaitBulkCompletion polls all operations in a single loop until all reach terminal state.
// This checks all pending operations in each polling cycle, which prevents false timeouts
// that occur when waiting for operations sequentially.
func awaitBulkCompletion(action string, queuedOps []*goclientnew.QueuedOperation) map[string]error {
	results := make(map[string]error) // opID -> error (nil = success)
	pending := make(map[string]*goclientnew.QueuedOperation)
	unitSlugs := make(map[string]string) // opID -> unit slug for display

	// Initialize pending set and pre-fetch unit slugs
	for _, op := range queuedOps {
		opID := op.QueuedOperationID.String()
		pending[opID] = op
		// Pre-fetch unit slug for better error messages
		unitDetails, err := apiGetUnitInSpace(op.UnitID.String(), op.SpaceID.String(), "Slug")
		if err == nil && unitDetails != nil && unitDetails.Slug != "" {
			unitSlugs[opID] = unitDetails.Slug
		} else {
			unitSlugs[opID] = op.UnitID.String()
		}
	}

	timeoutDuration, err := time.ParseDuration(timeout)
	if err != nil {
		timeoutDuration = DefaultTimeoutDuration // default
	}
	// Backend polls at 100ms intervals, so we use slightly larger to ensure updates are available
	sleepDuration := 111 * time.Millisecond
	maxSleepDuration := sleepDuration * 32
	startTime := time.Now()

	for time.Since(startTime) < timeoutDuration && len(pending) > 0 {
		pendingBefore := len(pending)

		// Check all pending operations in this cycle
		for opID, op := range pending {
			whereQueuedOp := "QueuedOperationID='" + opID + "'"

			// Query events for this specific operation
			events, err := apiListUnitEvents(op.SpaceID, op.UnitID, whereQueuedOp, "")
			if err != nil || len(events) == 0 {
				// Operation hasn't started yet
				continue
			}

			// Get the most recent event for this operation (first in list - they are sorted in apiListUnitEvents)
			event := events[0]

			status := actionStatus(event.Status)
			if isTerminalStatus(status) {
				delete(pending, opID)
				if !quiet && !isAlternativeOutput() && !hasAlternativeFunctionOutput() {
					displayOperationResults(op.UnitID.String(), event)
				}
				if status == goclientnew.ActionStatusTypeFailed {
					results[opID] = fmt.Errorf("%s failed on unit %s", action, unitSlugs[opID])
				} else {
					results[opID] = nil // success
				}
			}
		}

		if len(pending) > 0 {
			time.Sleep(sleepDuration)
			// Only increase backoff if no progress was made
			if len(pending) == pendingBefore {
				sleepDuration *= 2
				if sleepDuration > maxSleepDuration {
					sleepDuration = maxSleepDuration
				}
			}
		}
	}

	// Mark remaining pending operations as timed out
	for opID := range pending {
		results[opID] = fmt.Errorf("%s didn't complete on unit %s", action, unitSlugs[opID])
	}

	return results
}
