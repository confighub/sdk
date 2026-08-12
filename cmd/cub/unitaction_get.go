// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/base64"
	"fmt"
	"strconv"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

var unitActionGetCmd = &cobra.Command{
	Use:   "get <unit-slug> <unit-action-id-or-num>",
	Short: "Get a unit action",
	Long:  getCommandHelp("Get a unit action by its UUID or by its UnitActionNum (a per-unit sequence number).", ""),
	Args:  cobra.ExactArgs(2),
	RunE:  unitActionGetRun,
}

var showData bool

func init() {
	addStandardGetFlags(unitActionGetCmd)
	unitActionGetCmd.Flags().BoolVar(&showData, "data", false, "decode and display the Data field")
	_ = unitActionGetCmd.Flags().MarkDeprecated("data", "use 'cub unit-action data'")
	unitActionCmd.AddCommand(unitActionGetCmd)
}

func unitActionGetRun(cmd *cobra.Command, args []string) error {
	slug := args[0]
	identifier := args[1]

	u, err := apiGetUnitFromSlug(slug, "*") // get all fields for now
	if err != nil {
		return err
	}

	var action *goclientnew.UnitAction
	if actionUUID, uuidErr := uuid.Parse(identifier); uuidErr == nil {
		action, err = apiGetUnitAction(uuid.MustParse(selectedSpaceID), u.UnitID, actionUUID)
		if err != nil {
			return err
		}
	} else if num, numErr := strconv.ParseInt(identifier, 10, 64); numErr == nil {
		action, err = apiGetUnitActionFromNum(uuid.MustParse(selectedSpaceID), u.UnitID, num)
		if err != nil {
			return err
		}
	} else {
		return fmt.Errorf("unit-action identifier must be a UUID or UnitActionNum: %q", identifier)
	}

	unitActionUserLookup, err = fetchUsersForActions([]*goclientnew.UnitAction{action})
	if err != nil {
		return err
	}

	displayGetResults(action, displayUnitAction)

	return nil
}

// apiGetUnitActionFromNum finds a UnitAction by its per-unit UnitActionNum by issuing a
// list call scoped to the unit with a UnitActionNum where clause.
func apiGetUnitActionFromNum(spaceID uuid.UUID, unitID uuid.UUID, num int64) (*goclientnew.UnitAction, error) {
	whereClause := fmt.Sprintf("UnitActionNum = %d", num)
	actions, err := apiListUnitActions(spaceID, unitID, whereClause, "")
	if err != nil {
		return nil, err
	}
	for _, a := range actions {
		if a.UnitActionNum == num {
			return a, nil
		}
	}
	return nil, fmt.Errorf("unit action num %d not found for unit %s", num, unitID)
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
	table.Append([]string{"DryRun", fmt.Sprintf("%v", unitAction.DryRun)})
	table.Append([]string{"Status", string(unitAction.Status)})
	table.Append([]string{"Created At", unitAction.CreatedAt.String()})
	table.Append([]string{"User ID", unitAction.UserID.String()})
	username := ""
	if unitAction.UserID != uuid.Nil {
		if u, ok := unitActionUserLookup[unitAction.UserID]; ok && u != nil {
			username = u.Username
		}
	}
	table.Append([]string{"Username", username})
	table.Append([]string{"Bridge Worker ID", unitAction.BridgeWorkerID.String()})
	table.Append([]string{"RevisionNum", fmt.Sprintf("%v", unitAction.RevisionNum)})
	table.Append([]string{"UnitActionNum", fmt.Sprintf("%v", unitAction.UnitActionNum)})
	table.Render()

	if len(unitAction.ErrorDetails) > 0 {
		tprintRaw("")
		tprintRaw("ErrorDetails:")
		tprintRaw("-------------")
		for _, item := range unitAction.ErrorDetails {
			if item.Item != "" {
				tprintRaw(fmt.Sprintf("  %s: %s", item.Item, item.Description))
			} else {
				tprintRaw(fmt.Sprintf("  %s", item.Description))
			}
		}
	}

	if showData && len(unitAction.Data) > 0 {
		tprintRaw("")
		tprintRaw("Data:")
		tprintRaw("-----")
		dataBytes, err := base64.StdEncoding.DecodeString(unitAction.Data)
		failOnError(err)
		tprintRaw(string(dataBytes))
	}
}
