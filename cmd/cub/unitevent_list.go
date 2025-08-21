// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"sort"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
)

var unitEventListCmd = &cobra.Command{
	Use:   "list <unit-slug>",
	Short: "List unit events",
	Args:  cobra.ExactArgs(1),
	RunE:  unitEventListRun,
}

// Default columns to display when no custom columns are specified
var defaultUnitEventColumns = []string{"Action", "Result", "Status", "CreatedAt", "TerminatedAt", "Message"}

// UnitEvent-specific aliases
var unitEventAliases = map[string]string{
	"ID": "UnitEventID",
}

// UnitEvent custom column dependencies
var unitEventCustomColumnDependencies = map[string][]string{}

func init() {
	addStandardListFlags(unitEventListCmd)
	unitEventCmd.AddCommand(unitEventListCmd)
}

func unitEventListRun(cmd *cobra.Command, args []string) error {
	slug := args[0]
	u, err := apiGetUnitFromSlug(slug, "*") // get all fields for now
	if err != nil {
		return err
	}

	actions, err := apiListUnitEvents(uuid.MustParse(selectedSpaceID), u.UnitID, where)
	if err != nil {
		return err
	}

	displayListResults(actions, getUnitEventSlug, displayUnitEventList)
	return nil
}

func getUnitEventSlug(entity *goclientnew.UnitEvent) string {
	return "-"
}

func displayUnitEventList(events []*goclientnew.UnitEvent) {
	table := tableView()
	if !noheader {
		table.SetHeader([]string{"Action", "Result", "Status", "Created-At", "Terminated-At", "Message"})
	}
	for _, action := range events {
		act := ""
		result := ""
		if action.Action != nil {
			act = string(*action.Action)
		}
		if action.Result != nil {
			result = string(*action.Result)
		}
		terminatedAt := ""
		if !action.TerminatedAt.IsZero() {
			terminatedAt = action.TerminatedAt.String()
		}
		table.Append([]string{
			act,
			result,
			string(actionStatus(action.Status)),
			action.CreatedAt.String(),
			terminatedAt,
			action.Message,
		})
	}
	table.Render()
}

func apiListUnitEvents(spaceID uuid.UUID, unitID uuid.UUID, whereFilter string) ([]*goclientnew.UnitEvent, error) {
	newParams := &goclientnew.ListUnitEventsParams{}
	if whereFilter != "" {
		newParams.Where = &whereFilter
	}
	if contains != "" {
		newParams.Contains = &contains
	}
	// TODO: Add select parameter support when backend endpoint supports it
	// Auto-select fields based on default display if no custom output format is specified
	// if selectFields == "" {
	//     baseFields := []string{"UnitEventID", "UnitID", "SpaceID", "OrganizationID"}
	//     autoSelect := buildSelectList("UnitEvent", "", "", defaultUnitEventColumns, unitEventAliases, unitEventCustomColumnDependencies, baseFields)
	//     newParams.Select = &autoSelect
	// } else if selectFields != "" {
	//     newParams.Select = &selectFields
	// }
	eventsRes, err := cubClientNew.ListUnitEventsWithResponse(ctx, spaceID, unitID, newParams)
	if IsAPIError(err, eventsRes) {
		return nil, InterpretErrorGeneric(err, eventsRes)
	}
	events := make([]*goclientnew.UnitEvent, 0, len(*eventsRes.JSON200))
	for _, event := range *eventsRes.JSON200 {
		events = append(events, &event)
	}

	// Sort by CreatedAt descending (most recent first)
	sort.Slice(events, func(i, j int) bool {
		return events[i].CreatedAt.After(events[j].CreatedAt)
	})

	return events, nil
}
