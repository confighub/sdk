// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/google/uuid"
	"github.com/spf13/cobra"

	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
)

var linkCreateCmd = &cobra.Command{
	Use:   "create <link slug> <from unit slug> <to unit slug> [<to space slug>]",
	Short: "Create a new link",
	Long: `Create a new link between two units. Links define relationships between units and can be used to establish dependencies or connections between resources.

A link can be created:
  1. Between units in the same space
  2. Between units across different spaces (by specifying the target space)

Examples:
  # Create a link between a deployment and its namespace in the same space
  cub link create --space my-space --json to-ns my-deployment my-ns --wait

  # Create a link for a complex application to its namespace
  cub link create --space my-space --json headlamp-to-ns headlamp my-ns --wait

  # Create a link between a cloned unit and a namespace
  cub link create --space my-space --json clone-to-ns my-clone my-ns --wait`,
	Args: cobra.RangeArgs(3, 4),
	RunE: linkCreateCmdRun,
}

func init() {
	addStandardCreateFlags(linkCreateCmd)
	enableWaitFlag(linkCreateCmd)
	linkCmd.AddCommand(linkCreateCmd)
}

func linkCreateCmdRun(cmd *cobra.Command, args []string) error {
	newLink := &goclientnew.Link{}
	if flagPopulateModelFromStdin {
		if err := populateNewModelFromStdin(newLink); err != nil {
			return err
		}
	}
	err := setLabels(&newLink.Labels)
	if err != nil {
		return err
	}
	newLink.SpaceID = uuid.MustParse(selectedSpaceID)
	newLink.Slug = makeSlug(args[0])
	if newLink.DisplayName == "" {
		newLink.DisplayName = args[0]
	}

	fromUnit, err := apiGetUnitFromSlug(args[1])
	if err != nil {
		return err
	}
	fromUnitID := fromUnit.UnitID
	toSpaceID := selectedSpaceID
	if len(args) == 4 {
		toSpace, err := apiGetSpaceFromSlug(args[3])
		if err != nil {
			return err
		}
		toSpaceID = toSpace.SpaceID.String()
	}
	toUnit, err := apiGetUnitFromSlugInSpace(args[2], toSpaceID)
	if err != nil {
		return err
	}
	toUnitID := toUnit.UnitID

	newLink.FromUnitID = fromUnitID
	newLink.ToUnitID = toUnitID
	newLink.ToSpaceID = uuid.MustParse(toSpaceID)

	linkRes, err := cubClientNew.CreateLinkWithResponse(ctx, uuid.MustParse(selectedSpaceID), *newLink)
	if IsAPIError(err, linkRes) {
		return InterpretErrorGeneric(err, linkRes)
	}
	linkDetails := linkRes.JSON200
	displayCreateResults(linkDetails, "link", args[0], linkDetails.LinkID.String(), displayLinkDetails)
	if wait {
		if !quiet {
			tprint("Awaiting triggers...")
		}
		unitDetails, err := apiGetUnit(fromUnitID.String())
		if err != nil {
			return err
		}
		err = awaitTriggersRemoval(unitDetails)
		if err != nil {
			return err
		}
	}
	return err
}
