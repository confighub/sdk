// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/google/uuid"
	"github.com/spf13/cobra"

	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
)

var unitEventGetCmd = &cobra.Command{
	Use:   "get <unit-slug> <unit-event-id>",
	Short: "Get a unit event",
	Args:  cobra.ExactArgs(2),
	RunE:  unitEventGetRun,
}

func init() {
	addStandardGetFlags(unitEventGetCmd)
	unitEventCmd.AddCommand(unitEventGetCmd)
}

func unitEventGetRun(cmd *cobra.Command, args []string) error {
	slug := args[0]
	eventID := args[1]

	u, err := apiGetUnitFromSlug(slug, "*") // get all fields for now
	if err != nil {
		return err
	}

	eventUUID, err := uuid.Parse(eventID)
	if err != nil {
		return err
	}

	event, err := apiGetUnitEvent(uuid.MustParse(selectedSpaceID), u.UnitID, eventUUID)
	if err != nil {
		return err
	}

	displayGetResults(event, displayUnitEvent)
	return nil
}

func apiGetUnitEvent(spaceID uuid.UUID, unitID uuid.UUID, eventID uuid.UUID) (*goclientnew.UnitEvent, error) {
	// No params yet
	eventRes, err := cubClientNew.GetUnitEventWithResponse(ctx, spaceID, unitID, eventID)
	if IsAPIError(err, eventRes) {
		return nil, InterpretErrorGeneric(err, eventRes)
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
}
