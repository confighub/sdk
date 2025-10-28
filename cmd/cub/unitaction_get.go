// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/confighub/sdk/cubapi"
	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
)

var unitActionGetCmd = &cobra.Command{
	Use:   "get <unit-slug> <unit-action-id>",
	Short: "Get a unit action",
	Long:  getCommandHelp("Get a unit action", ""),
	Args:  cobra.ExactArgs(2),
	RunE:  unitActionGetRun,
}

func init() {
	addStandardGetFlags(unitActionGetCmd)
	unitActionCmd.AddCommand(unitActionGetCmd)
}

func unitActionGetRun(cmd *cobra.Command, args []string) error {
	slug := args[0]
	actionID := args[1]

	u, err := apiGetUnitFromSlug(slug, "*") // get all fields for now
	if err != nil {
		return err
	}

	actionUUID, err := uuid.Parse(actionID)
	if err != nil {
		return err
	}

	action, err := apiGetUnitAction(uuid.MustParse(selectedSpaceID), u.UnitID, actionUUID)
	if err != nil {
		return err
	}

	displayGetResults(action, displayUnitAction)
	return nil
}

func apiGetUnitAction(spaceID uuid.UUID, unitID uuid.UUID, actionID uuid.UUID) (*goclientnew.UnitAction, error) {
	// No params yet
	actionRes, err := cubClientNew.GetUnitActionWithResponse(ctx, spaceID, unitID, actionID)
	if cubapi.IsAPIError(err, actionRes) {
		return nil, cubapi.InterpretErrorGeneric(err, actionRes)
	}
	return actionRes.JSON200, nil
}

func displayUnitAction(unitAction *goclientnew.UnitAction) {
	table := tableView()

	action := ""
	if unitAction.Action != nil {
		action = string(*unitAction.Action)
	}
	table.Append([]string{"Action", action})
	table.Append([]string{"Status", string(unitAction.Status)})
	table.Append([]string{"Created At", unitAction.CreatedAt.String()})
	table.Append([]string{"User ID", unitAction.UserID.String()})
	table.Append([]string{"Bridge Worker ID", unitAction.BridgeWorkerID.String()})

	table.Render()
}
