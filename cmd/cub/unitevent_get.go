// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"sort"
	"strconv"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

var unitEventGetCmd = &cobra.Command{
	Use:   "get <unit-slug> <unit-event-id-or-num>",
	Short: "Get a unit event",
	Long:  getCommandHelp("Get a unit event by its UUID or by its UnitEventNum (a per-unit sequence number).", ""),
	Args:  cobra.ExactArgs(2),
	RunE:  unitEventGetRun,
}

func init() {
	addStandardGetFlags(unitEventGetCmd)
	unitEventCmd.AddCommand(unitEventGetCmd)
}

func unitEventGetRun(cmd *cobra.Command, args []string) error {
	slug := args[0]
	identifier := args[1]

	u, err := apiGetUnitFromSlug(slug, "*") // get all fields for now
	if err != nil {
		return err
	}

	var event *goclientnew.UnitEvent
	if eventUUID, uuidErr := uuid.Parse(identifier); uuidErr == nil {
		event, err = apiGetUnitEvent(uuid.MustParse(selectedSpaceID), u.UnitID, eventUUID)
		if err != nil {
			return err
		}
	} else if num, numErr := strconv.ParseInt(identifier, 10, 64); numErr == nil {
		event, err = apiGetUnitEventFromNum(uuid.MustParse(selectedSpaceID), u.UnitID, num)
		if err != nil {
			return err
		}
	} else {
		return fmt.Errorf("unit-event identifier must be a UUID or UnitEventNum: %q", identifier)
	}

	displayGetResults(event, displayUnitEvent)
	return nil
}

// apiGetUnitEventFromNum finds a UnitEvent by its per-unit UnitEventNum by issuing a
// list call scoped to the unit with a UnitEventNum where clause.
func apiGetUnitEventFromNum(spaceID uuid.UUID, unitID uuid.UUID, num int64) (*goclientnew.UnitEvent, error) {
	whereClause := fmt.Sprintf("UnitEventNum = %d", num)
	events, err := apiListUnitEvents(spaceID, unitID, whereClause, "")
	if err != nil {
		return nil, err
	}
	for _, e := range events {
		if e.UnitEventNum == num {
			return e, nil
		}
	}
	return nil, fmt.Errorf("unit event num %d not found for unit %s", num, unitID)
}

func apiGetUnitEvent(spaceID uuid.UUID, unitID uuid.UUID, eventID uuid.UUID) (*goclientnew.UnitEvent, error) {
	// No params yet
	eventRes, err := cubClientNew.GetUnitEventWithResponse(ctx, spaceID, unitID, eventID)
	if cubapi.IsAPIError(err, eventRes) {
		return nil, cubapi.InterpretErrorGeneric(err, eventRes)
	}
	return eventRes.JSON200, nil
}

func displayUnitEvent(event *goclientnew.UnitEvent) {
	table := tableView()

	action := ""
	result := ""
	if event.Action != nil {
		action = string(*event.Action)
	}
	if event.Result != nil {
		result = string(*event.Result)
	}

	table.Append([]string{"UnitEventNum", fmt.Sprintf("%v", event.UnitEventNum)})
	table.Append([]string{"RevisionNum", fmt.Sprintf("%v", event.RevisionNum)})
	table.Append([]string{"Action", action})
	table.Append([]string{"Result", result})
	table.Append([]string{"Status", string(actionStatus(event.Status))})
	table.Append([]string{"Created At", event.CreatedAt.String()})
	table.Append([]string{"Terminated At", event.TerminatedAt.String()})
	table.Append([]string{"Message", event.Message})

	// Display BridgeWorkerID if present
	if event.BridgeWorkerID != nil {
		table.Append([]string{"Bridge Worker ID", event.BridgeWorkerID.String()})
	}

	table.Render()

	if event.ResourceStatuses != nil && len(*event.ResourceStatuses) > 0 {
		tprintRaw("")
		tprintRaw("ResourceStatuses:")
		tprintRaw("-----------------")
		keys := make([]string, 0, len(*event.ResourceStatuses))
		for k := range *event.ResourceStatuses {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		rsTable := tableView()
		rsTable.SetHeader([]string{"Resource", "SyncStatus", "Readiness", "Updated-At", "Message"})
		for _, k := range keys {
			rs := (*event.ResourceStatuses)[k]
			updatedAt := ""
			if !rs.UpdatedAt.IsZero() {
				updatedAt = rs.UpdatedAt.String()
			}
			rsTable.Append([]string{k, rs.SyncStatus, rs.Readiness, updatedAt, rs.Message})
		}
		rsTable.Render()
	}
}
