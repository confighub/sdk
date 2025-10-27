// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"sort"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/confighub/sdk/cubapi"
	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
)

var unitActionListCmd = &cobra.Command{
	Use:   "list <unit-slug>",
	Short: "List unit actions",
	Args:  cobra.ExactArgs(1),
	RunE:  unitActionListRun,
}

// Default columns to display when no custom columns are specified
var defaultUnitActionColumns = []string{"Action", "Status", "CreatedAt", "ID", "UserID"}

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

	slug := args[0]
	u, err := apiGetUnitFromSlug(slug, "*") // get all fields for now
	if err != nil {
		return err
	}

	actions, err := apiListUnitActions(uuid.MustParse(selectedSpaceID), u.UnitID, where, filterID)
	if err != nil {
		return err
	}

	displayListResults(actions, getUnitActionSlug, displayUnitActionList)
	return nil
}

func getUnitActionSlug(entity *goclientnew.UnitAction) string {
	return "-"
}

func displayUnitActionList(actions []*goclientnew.UnitAction) {
	table := tableView()
	if !noheader {
		table.SetHeader([]string{"Action", "Status", "Created-At", "ID", "UserID"})
	}
	for _, action := range actions {
		act := ""
		if action.Action != nil {
			act = string(*action.Action)
		}
		table.Append([]string{
			act,
			string(action.Status),
			action.CreatedAt.String(),
			action.QueuedOperationID.String(),
			action.UserID.String(), // todo: get user
		})
	}
	table.Render()
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
