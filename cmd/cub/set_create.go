// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var setCreateCmd = &cobra.Command{
	Use:   "create <slug>",
	Short: "Create a new set",
	Long: `Create a new set to group and organize related units within a space.

Sets provide a way to:
  1. Organize units logically
  2. Apply common configurations across grouped units
  3. Manage permissions and access controls for groups of units

Examples:
  # Create a new set with JSON output, reading configuration from stdin
  cub set create --space my-space --json --from-stdin my-set`,
	Args: cobra.ExactArgs(1),
	RunE: setCreateCmdRun,
}

func init() {
	addStandardCreateFlags(setCreateCmd)
	setCmd.AddCommand(setCreateCmd)
}

func setCreateCmdRun(cmd *cobra.Command, args []string) error {
	newSet := &goclientnew.Set{}
	if flagPopulateModelFromStdin {
		if err := populateNewModelFromStdin(newSet); err != nil {
			return err
		}
	}
	err := setLabels(&newSet.Labels)
	if err != nil {
		return err
	}
	spaceID := uuid.MustParse(selectedSpaceID)
	newSet.SpaceID = spaceID
	newSet.Slug = makeSlug(args[0])
	if newSet.DisplayName == "" {
		newSet.DisplayName = args[0]
	}

	setRes, err := cubClientNew.CreateSetWithResponse(ctx, spaceID, *newSet)
	if IsAPIError(err, setRes) {
		return InterpretErrorGeneric(err, setRes)
	}
	setDetails := setRes.JSON200
	displayCreateResults(setDetails, "set", args[0], setDetails.SetID.String(), displaySetDetails)
	return nil
}
