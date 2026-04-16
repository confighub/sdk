// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

var unitEventListCmd = &cobra.Command{
	Use:   "list [unit-slug]",
	Short: "List unit events",
	Long: getCommandHelp(`List unit events for a specific unit, or across all units when no unit is specified. Use --space '*' to search across all spaces in the Organization.

Examples:
`+"```"+`
  # List unit events for a specific unit
  cub unit-event list --space my-space my-unit

  # List failed Apply events in the space
  cub unit-event list --space my-space --where "Action = 'Apply' AND Result = 'Failed'"

  # List Apply events started within the past 15 minutes across all spaces
  cub unit-event list --space '*' --where "Action = 'Apply' AND CreatedAt > '2026-04-15T12:00:00Z'"
`+"```"+`
`, ""),
	Args:        cobra.RangeArgs(0, 1),
	RunE:        unitEventListRun,
	Annotations: map[string]string{"OrgLevel": ""},
}

// Default columns to display when no custom columns are specified
var defaultUnitEventColumns = []string{"Action", "Result", "Status", "CreatedAt", "TerminatedAt", "Message"}

// Package-level lookup so displayUnitEventList can render Unit/Space slugs without
// changing the shared displayListResults signature.
var unitEventUnitLookup map[uuid.UUID]*goclientnew.Unit

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
	filterID, err := parseFilterFlag(filter)
	if err != nil {
		return err
	}

	// Bulk mode: no unit-slug positional argument
	if len(args) == 0 {
		effectiveWhere := where
		if selectedSpaceID != "*" {
			effectiveWhere = addSpaceIDToWhereClause(effectiveWhere, selectedSpaceID)
		}
		events, err := apiSearchListUnitEvents(effectiveWhere, filterID)
		if err != nil {
			return err
		}
		unitEventUnitLookup, err = fetchUnitsForEvents(events)
		if err != nil {
			return err
		}
		displayListResults(events, getUnitEventSlug, displayUnitEventList)
		return nil
	}

	slug := args[0]
	u, err := apiGetUnitFromSlug(slug, "*") // get all fields for now
	if err != nil {
		return err
	}

	events, err := apiListUnitEvents(uuid.MustParse(selectedSpaceID), u.UnitID, where, filterID)
	if err != nil {
		return err
	}

	unitEventUnitLookup = map[uuid.UUID]*goclientnew.Unit{u.UnitID: u}
	displayListResults(events, getUnitEventSlug, displayUnitEventList)
	return nil
}

// fetchUnitsForEvents looks up the unique Units referenced by the given unit events so
// their Slug and SpaceSlug can be displayed alongside each event.
func fetchUnitsForEvents(events []*goclientnew.UnitEvent) (map[uuid.UUID]*goclientnew.Unit, error) {
	lookup := make(map[uuid.UUID]*goclientnew.Unit)
	if len(events) == 0 {
		return lookup, nil
	}
	seen := make(map[uuid.UUID]struct{})
	quoted := make([]string, 0, len(events))
	for _, event := range events {
		if _, ok := seen[event.UnitID]; ok {
			continue
		}
		seen[event.UnitID] = struct{}{}
		quoted = append(quoted, fmt.Sprintf("'%s'", event.UnitID.String()))
	}
	whereClause := fmt.Sprintf("UnitID IN (%s)", strings.Join(quoted, ","))
	extendedUnits, err := apiSearchUnits(whereClause, "", "", "", "", false, "Slug,UnitID,SpaceID,OrganizationID,SpaceSlug", "", "")
	if err != nil {
		return nil, err
	}
	for _, eu := range extendedUnits {
		if eu.Unit != nil {
			lookup[eu.Unit.UnitID] = eu.Unit
		}
	}
	return lookup, nil
}

func getUnitEventSlug(entity *goclientnew.UnitEvent) string {
	return "-"
}

func displayUnitEventList(events []*goclientnew.UnitEvent) {
	table := tableView()
	if !noheader {
		table.SetHeader([]string{"Num", "Action", "Result", "Status", "Created-At", "Terminated-At", "Message", "Unit", "Space"})
	}
	for _, event := range events {
		act := ""
		result := ""
		if event.Action != nil {
			act = string(*event.Action)
		}
		if event.Result != nil {
			result = string(*event.Result)
		}
		terminatedAt := ""
		if !event.TerminatedAt.IsZero() {
			terminatedAt = event.TerminatedAt.String()
		}
		unitSlug := ""
		spaceSlug := ""
		if u, ok := unitEventUnitLookup[event.UnitID]; ok && u != nil {
			unitSlug = u.Slug
			spaceSlug = u.SpaceSlug
		}
		table.Append([]string{
			fmt.Sprintf("%d", event.UnitEventNum),
			act,
			result,
			string(actionStatus(event.Status)),
			event.CreatedAt.String(),
			terminatedAt,
			event.Message,
			unitSlug,
			spaceSlug,
		})
	}
	table.Render()
}

func apiSearchListUnitEvents(whereFilter string, filterParam string) ([]*goclientnew.UnitEvent, error) {
	newParams := &goclientnew.ListAllUnitEventsParams{}
	if whereFilter != "" {
		newParams.Where = &whereFilter
	}
	if filterParam != "" {
		newParams.Filter = &filterParam
	}
	if contains != "" {
		newParams.Contains = &contains
	}
	eventsRes, err := cubClientNew.ListAllUnitEventsWithResponse(ctx, newParams)
	if cubapi.IsAPIError(err, eventsRes) {
		return nil, cubapi.InterpretErrorGeneric(err, eventsRes)
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

func apiListUnitEvents(spaceID uuid.UUID, unitID uuid.UUID, whereFilter string, filterParam string) ([]*goclientnew.UnitEvent, error) {
	newParams := &goclientnew.ListUnitEventsParams{}
	if whereFilter != "" {
		newParams.Where = &whereFilter
	}
	if filterParam != "" {
		newParams.Filter = &filterParam
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
	if cubapi.IsAPIError(err, eventsRes) {
		return nil, cubapi.InterpretErrorGeneric(err, eventsRes)
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
