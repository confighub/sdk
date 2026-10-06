// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"errors"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/spf13/cobra"
)

var componentDeleteCmd = &cobra.Command{
	Use:   "delete [<name or id>]",
	Short: "Delete a component or multiple components",
	Args:  cobra.MaximumNArgs(1),
	Long: getCommandHelp(`Delete a Component entity, or the Components --where selects.

Examples:
`+"```"+`
  cub component delete my-app

  # Delete the components whose backing Units are empty, keeping the Units
  cub component delete --from-backing-units --where "Labels.team = 'platform'"
`+"```"+`
`, ""),
	RunE: componentDeleteCmdRun,
}

func init() {
	addStandardDeleteFlags(componentDeleteCmd)
	enableWhereFlag(componentDeleteCmd)
	addPruneFlag(componentDeleteCmd, "Component")
	componentCmd.AddCommand(componentDeleteCmd)
}

func runBulkComponentDelete() error {
	params := &goclientnew.BulkDeleteComponentsParams{Where: &where}
	params.IncludeHidden = includeHiddenParam()
	params.FromBackingUnits = fromBackingUnitsParam()
	bulkRes, err := cubClientNew.BulkDeleteComponentsWithResponse(ctx, params)
	if cubapi.IsAPIError(err, bulkRes) {
		return cubapi.InterpretErrorGeneric(err, bulkRes)
	}
	return displayBulkDeleteResults(bulkRes.JSON200, bulkRes.JSON207, bulkRes.StatusCode(), "component", deleteOperationName(), where)
}

func componentDeleteCmdRun(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		if where == "" && !backingUnitArgs.fromBackingUnits {
			return errors.New("name a component, or select components with --where")
		}
		return runBulkComponentDelete()
	}
	if where != "" {
		return errors.New("--where can only be specified with no positional arguments")
	}
	componentDetails, err := resolveComponent(args[0], "ComponentID,Slug")
	if err != nil {
		return err
	}
	componentID := componentDetails.Component.ComponentID
	deleteRes, err := cubClientNew.DeleteComponentWithResponse(ctx, componentID)
	if cubapi.IsAPIError(err, deleteRes) {
		return cubapi.InterpretErrorGeneric(err, deleteRes)
	}
	displayDeleteResults("component", args[0], componentID.String(), deleteRes)
	return nil
}
