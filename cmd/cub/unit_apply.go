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

var unitApplyCmd = &cobra.Command{
	Use:   "apply <unit-slug>",
	Args:  cobra.ExactArgs(1),
	Short: "Apply a configuration unit to the target",
	Long:  "Apply a configuration unit to the target",
	RunE:  unitApplyCmdRun,
}

func init() {
	enableWaitFlag(unitApplyCmd)
	enableQuietFlagForOperation(unitApplyCmd)
	unitCmd.AddCommand(unitApplyCmd)
}

func unitApplyCmdRun(_ *cobra.Command, args []string) error {
	configUnit, err := apiGetUnitFromSlug(args[0])
	if err != nil {
		return err
	}

	applyRes, err := cubClientNew.ApplyUnitWithResponse(ctx, uuid.MustParse(selectedSpaceID), configUnit.UnitID)
	if IsAPIError(err, applyRes) {
		return InterpretErrorGeneric(err, applyRes)
	}
	if wait {
		return awaitCompletion("apply", applyRes.JSON200)
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
	if actionStatus(event.Status) == goclientnew.ActionStatusTypeCompleted {
		tprint("Successfully completed %s on unit %s", actionType(event.Action), id)
		return
	}
	tprint("Action %s on unit %s %s", actionType(event.Action), id, actionStatus(event.Status))
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
			extendedUnit, err := apiGetExtendedUnitFromSlugInSpace(unitIDString, spaceIDString)
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
	unitDetails, err := apiGetUnit(unitIDString)
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
