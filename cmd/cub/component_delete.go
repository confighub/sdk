// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/confighub/sdk/core/cubapi"
	"github.com/spf13/cobra"
)

var componentDeleteCmd = &cobra.Command{
	Use:   "delete <name or id>",
	Short: "Delete a component",
	Args:  cobra.ExactArgs(1),
	Long: getCommandHelp(`Delete a Component entity.

Examples:
`+"```"+`
  cub component delete my-app
`+"```"+`
`, ""),
	RunE: componentDeleteCmdRun,
}

func init() {
	addStandardDeleteFlags(componentDeleteCmd)
	componentCmd.AddCommand(componentDeleteCmd)
}

func componentDeleteCmdRun(cmd *cobra.Command, args []string) error {
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
