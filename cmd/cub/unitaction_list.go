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

var unitActionListCmd = &cobra.Command{
	Use:   "list [unit-slug]",
	Short: "List unit actions",
	Long: getCommandHelp(`List unit actions for a specific unit, or across all units when no unit is specified. When listing across units, at most one unit action is returned per unit (the latest one, ordered by UnitActionNum). Use --space '*' to search across all spaces in the Organization.

Examples:
`+"```"+`
  # List unit actions for a specific unit
  cub unit-action list --space my-space my-unit

  # List the latest Apply unit action per unit in the space
  cub unit-action list --space my-space --where "Action = 'Apply'"

  # List the latest Apply unit actions started within the past 15 minutes across all spaces
  cub unit-action list --space '*' --where "Action = 'Apply' AND CreatedAt > '2026-04-15T12:00:00Z'"
`+"```"+`
`, ""),
	Args:        cobra.RangeArgs(0, 1),
	RunE:        unitActionListRun,
	Annotations: map[string]string{"OrgLevel": ""},
}

// Default columns to display when no custom columns are specified
var defaultUnitActionColumns = []string{"Action", "Status", "CreatedAt", "ID", "User", "Unit", "Space"}

// Package-level lookups so displayUnitActionList can render Unit/Space/User slugs without
// changing the shared displayListResults signature.
var (
	unitActionUnitLookup map[uuid.UUID]*goclientnew.Unit
	unitActionUserLookup map[uuid.UUID]*goclientnew.User
)

// UnitAction-specific aliases
var unitActionAliases = map[string]string{
	"ID": "QueuedOperationID",
}

// UnitAction custom column dependencies
var unitActionCustomColumnDependencies = map[string][]string{}

func init() {
	addStandardListFlags(unitActionListCmd)
	unitActionCmd.AddCommand(unitActionListCmd)
}

func unitActionListRun(cmd *cobra.Command, args []string) error {
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
		actions, err := apiSearchListUnitActions(effectiveWhere, filterID)
		if err != nil {
			return err
		}
		unitActionUnitLookup, err = fetchUnitsForActions(actions)
		if err != nil {
			return err
		}
		unitActionUserLookup, err = fetchUsersForActions(actions)
		if err != nil {
			return err
		}
		displayListResults(actions, getUnitActionSlug, displayUnitActionList)
		return nil
	}

	slug := args[0]
	u, err := apiGetUnitFromSlug(slug, "*") // get all fields for now
	if err != nil {
		return err
	}

	actions, err := apiListUnitActions(uuid.MustParse(selectedSpaceID), u.UnitID, where, filterID)
	if err != nil {
		return err
	}

	unitActionUnitLookup = map[uuid.UUID]*goclientnew.Unit{u.UnitID: u}
	unitActionUserLookup, err = fetchUsersForActions(actions)
	if err != nil {
		return err
	}
	displayListResults(actions, getUnitActionSlug, displayUnitActionList)
	return nil
}

// fetchUsersForActions looks up the unique Users referenced by the given unit actions so
// their Username can be displayed alongside each action.
func fetchUsersForActions(actions []*goclientnew.UnitAction) (map[uuid.UUID]*goclientnew.User, error) {
	lookup := make(map[uuid.UUID]*goclientnew.User)
	if len(actions) == 0 {
		return lookup, nil
	}
	seen := make(map[uuid.UUID]struct{})
	quoted := make([]string, 0, len(actions))
	for _, action := range actions {
		if action.UserID == uuid.Nil {
			continue
		}
		if _, ok := seen[action.UserID]; ok {
			continue
		}
		seen[action.UserID] = struct{}{}
		quoted = append(quoted, fmt.Sprintf("'%s'", action.UserID.String()))
	}
	if len(quoted) == 0 {
		return lookup, nil
	}
	whereClause := fmt.Sprintf("UserID IN (%s)", strings.Join(quoted, ","))
	users, err := apiListUsers(whereClause, "")
	if err != nil {
		return nil, err
	}
	for _, u := range users {
		if u != nil {
			lookup[u.UserID] = u
		}
	}
	return lookup, nil
}

// fetchUnitsForActions looks up the unique Units referenced by the given unit actions so
// their Slug and SpaceSlug can be displayed alongside each action.
func fetchUnitsForActions(actions []*goclientnew.UnitAction) (map[uuid.UUID]*goclientnew.Unit, error) {
	lookup := make(map[uuid.UUID]*goclientnew.Unit)
	if len(actions) == 0 {
		return lookup, nil
	}
	seen := make(map[uuid.UUID]struct{})
	quoted := make([]string, 0, len(actions))
	for _, action := range actions {
		if _, ok := seen[action.UnitID]; ok {
			continue
		}
		seen[action.UnitID] = struct{}{}
		quoted = append(quoted, fmt.Sprintf("'%s'", action.UnitID.String()))
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

func getUnitActionSlug(entity *goclientnew.UnitAction) string {
	return "-"
}

func displayUnitActionList(actions []*goclientnew.UnitAction) {
	table := tableView()
	if !noheader {
		table.SetHeader([]string{"Num", "Action", "Status", "Created-At", "ID", "User", "Unit", "Space"})
	}
	for _, action := range actions {
		act := ""
		if action.Action != nil {
			act = string(*action.Action)
		}
		unitSlug := ""
		spaceSlug := ""
		if u, ok := unitActionUnitLookup[action.UnitID]; ok && u != nil {
			unitSlug = u.Slug
			spaceSlug = u.SpaceSlug
		}
		username := ""
		if action.UserID != uuid.Nil {
			if u, ok := unitActionUserLookup[action.UserID]; ok && u != nil {
				username = u.Username
			} else {
				username = action.UserID.String()
			}
		}
		table.Append([]string{
			fmt.Sprintf("%d", action.UnitActionNum),
			act,
			string(action.Status),
			action.CreatedAt.String(),
			action.QueuedOperationID.String(),
			username,
			unitSlug,
			spaceSlug,
		})
	}
	table.Render()
}

func apiSearchListUnitActions(whereFilter string, filterParam string) ([]*goclientnew.UnitAction, error) {
	newParams := &goclientnew.ListAllUnitActionsParams{}
	if whereFilter != "" {
		newParams.Where = &whereFilter
	}
	if filterParam != "" {
		newParams.Filter = &filterParam
	}
	if contains != "" {
		newParams.Contains = &contains
	}
	actionsRes, err := cubClientNew.ListAllUnitActionsWithResponse(ctx, newParams)
	if cubapi.IsAPIError(err, actionsRes) {
		return nil, cubapi.InterpretErrorGeneric(err, actionsRes)
	}
	actions := make([]*goclientnew.UnitAction, 0, len(*actionsRes.JSON200))
	for _, action := range *actionsRes.JSON200 {
		actions = append(actions, &action)
	}

	// Sort by CreatedAt descending (most recent first)
	sort.Slice(actions, func(i, j int) bool {
		return actions[i].CreatedAt.After(actions[j].CreatedAt)
	})

	return actions, nil
}

func apiListUnitActions(spaceID uuid.UUID, unitID uuid.UUID, whereFilter string, filterParam string) ([]*goclientnew.UnitAction, error) {
	newParams := &goclientnew.ListUnitActionsParams{}
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
	//     baseFields := []string{"UnitActionID", "UnitID", "SpaceID", "OrganizationID"}
	//     autoSelect := buildSelectList("UnitAction", "", "", defaultUnitActionColumns, unitActionAliases, unitActionCustomColumnDependencies, baseFields)
	//     newParams.Select = &autoSelect
	// } else if selectFields != "" {
	//     newParams.Select = &selectFields
	// }
	actionsRes, err := cubClientNew.ListUnitActionsWithResponse(ctx, spaceID, unitID, newParams)
	if cubapi.IsAPIError(err, actionsRes) {
		return nil, cubapi.InterpretErrorGeneric(err, actionsRes)
	}
	actions := make([]*goclientnew.UnitAction, 0, len(*actionsRes.JSON200))
	for _, action := range *actionsRes.JSON200 {
		actions = append(actions, &action)
	}

	// Sort by CreatedAt descending (most recent first)
	sort.Slice(actions, func(i, j int) bool {
		return actions[i].CreatedAt.After(actions[j].CreatedAt)
	})

	return actions, nil
}
